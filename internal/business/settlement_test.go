package business

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Fixtures use transaction-local temporary tables; application tables are never reset.
func TestStagedOrderAtomicSettlement(t *testing.T) {
	url := os.Getenv("ORDER_SETTLEMENT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires disposable ORDER_SETTLEMENT_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fixture, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Rollback(ctx)
	_, err = fixture.Exec(ctx, `SET LOCAL search_path TO pg_temp;
 CREATE TEMP TABLE settings(settings text primary key,value text,updated_at timestamptz);
 INSERT INTO settings VALUES('office_money_balance','1000',now());
 CREATE TEMP TABLE business_orders(id bigint primary key,direction text,status text,total_price bigint);
 CREATE TEMP TABLE business_order_items(id bigint primary key,order_id bigint,stock_item_id bigint,quantity bigint,stock_units_per_unit bigint);
 CREATE TEMP TABLE safebox_stock_balances(safebox text,item_id bigint,quantity int,updated_at timestamptz);
 CREATE TEMP TABLE money_transactions(id bigint generated always as identity,account_type text,actor_member_id bigint,transaction_type text,direction text,amount bigint,balance_before bigint,balance_after bigint,reason text,business_order_id bigint);
 CREATE TEMP TABLE safebox_stock_transactions(safebox text,item_id bigint,action text,quantity_before int,quantity_after int,delta int,reason text,actor_member_id bigint,business_order_item_id bigint);
 INSERT INTO business_orders VALUES(1,'sale','open',200),(2,'purchase','open',300),(3,'sale','open',100),(4,'purchase','open',9999);
 INSERT INTO business_order_items VALUES(1,1,1,2,1),(2,2,1,1,180),(3,3,1,999,1),(4,4,1,1,1);
 INSERT INTO safebox_stock_balances VALUES('boss',1,10,now());`)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(id int64, status string, wantError bool) {
		t.Helper()
		tx, err := fixture.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, err = SettleOrderStatus(ctx, tx, id, 1, "boss", status)
		if wantError {
			if err == nil {
				t.Fatal("expected rejection")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, `UPDATE business_orders SET status=$2 WHERE id=$1`, id, status); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	check := func(wantMoney string, wantStock, wantEntries int) {
		t.Helper()
		var money string
		var stock, entries int
		if err := fixture.QueryRow(ctx, `SELECT value FROM settings`).Scan(&money); err != nil {
			t.Fatal(err)
		}
		if err := fixture.QueryRow(ctx, `SELECT quantity FROM safebox_stock_balances`).Scan(&stock); err != nil {
			t.Fatal(err)
		}
		if err := fixture.QueryRow(ctx, `SELECT COUNT(*) FROM money_transactions`).Scan(&entries); err != nil {
			t.Fatal(err)
		}
		if money != wantMoney || stock != wantStock || entries != wantEntries {
			t.Fatalf("money=%s stock=%d entries=%d", money, stock, entries)
		}
	}
	apply(1, "complete", true) // Cannot skip stock withdrawal.
	apply(1, "ready", false)
	check("1000", 8, 0)
	apply(1, "ready", false)
	check("1000", 8, 0) // Repeated stage creates nothing.
	apply(1, "open", true)
	apply(1, "complete", false)
	check("1200", 8, 1)
	apply(1, "complete", false)
	check("1200", 8, 1)
	apply(2, "paid", false)
	check("900", 8, 2)
	apply(2, "paid", false)
	check("900", 8, 2)
	apply(2, "open", true)
	apply(2, "complete", false)
	check("900", 188, 2)
	apply(2, "complete", false)
	apply(3, "ready", true) // Insufficient stock rolls back status and movement.
	apply(4, "paid", true)  // Insufficient funds never deposits stock.
	// Legacy Ready records without a stock withdrawal need reconciliation.
	if _, err := fixture.Exec(ctx, `UPDATE business_orders SET status='ready' WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	apply(3, "complete", true)

	var balance string
	var quantity, count int
	if err = fixture.QueryRow(ctx, `SELECT value FROM settings`).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err = fixture.QueryRow(ctx, `SELECT quantity FROM safebox_stock_balances`).Scan(&quantity); err != nil {
		t.Fatal(err)
	}
	if err = fixture.QueryRow(ctx, `SELECT count(*) FROM money_transactions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if balance != "900" || quantity != 188 || count != 2 {
		t.Fatalf("settlement/rollback mismatch: money=%s stock=%d entries=%d", balance, quantity, count)
	}

	// Completed orders cannot be cancelled; their balances remain unchanged.
	apply(1, "cancelled", true)
	apply(2, "cancelled", true)
	check("900", 188, 2)
	if _, err = fixture.Exec(ctx, `INSERT INTO business_orders VALUES(5,'sale','open',200),(6,'purchase','open',300); INSERT INTO business_order_items VALUES(5,5,1,2,1),(6,6,1,1,180);`); err != nil {
		t.Fatal(err)
	}
	apply(5, "ready", false)
	check("900", 186, 2)
	apply(5, "cancelled", false)
	check("900", 188, 2)
	apply(5, "cancelled", false)
	check("900", 188, 2)
	apply(5, "ready", true)
	apply(6, "paid", false)
	check("600", 188, 3)
	apply(6, "cancelled", false)
	check("900", 188, 4)

}

func TestOrderStatusMovement(t *testing.T) {
	for _, tc := range []struct{ direction, current, target, want string }{
		{"sale", "open", "ready", "boss"}, {"sale", "ready", "complete", "office"},
		{"purchase", "open", "paid", "office"}, {"purchase", "paid", "complete", "boss"},
		{"sale", "open", "cancelled", "cancel"}, {"sale", "ready", "cancelled", "cancel"}, {"purchase", "paid", "cancelled", "cancel"}, {"sale", "cancelled", "cancelled", ""}, {"sale", "ready", "ready", ""}, {"purchase", "paid", "paid", ""},
	} {
		got, err := OrderStatusMovement(tc.direction, tc.current, tc.target)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %s %v", tc, got, err)
		}
	}
	for _, tc := range [][3]string{{"sale", "open", "complete"}, {"purchase", "open", "complete"}, {"sale", "ready", "open"}, {"purchase", "paid", "open"}, {"sale", "open", "paid"}, {"purchase", "open", "ready"}, {"sale", "complete", "ready"}, {"sale", "complete", "cancelled"}, {"purchase", "complete", "cancelled"}} {
		if _, err := OrderStatusMovement(tc[0], tc[1], tc[2]); err == nil {
			t.Fatalf("accepted invalid transition: %v", tc)
		}
	}
}
