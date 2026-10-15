package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOrderNotificationQueueRollsBackWithSettlement(t *testing.T) {
	url := os.Getenv("ORDER_NOTIFICATION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires disposable ORDER_NOTIFICATION_TEST_DATABASE_URL")
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
	// Temporary tables shadow application tables and disappear on rollback.
	_, err = fixture.Exec(ctx, `SET LOCAL search_path TO pg_temp;
 CREATE TEMP TABLE money_transactions(id bigint,account_type text,direction text,amount bigint,balance_before bigint,balance_after bigint,reason text,actor_member_id bigint,business_order_id bigint);
 CREATE TEMP TABLE business_order_items(id bigint,order_id bigint);
 CREATE TEMP TABLE safebox_stock_items(id bigint,name text);
 CREATE TEMP TABLE safebox_stock_transactions(business_order_item_id bigint,item_id bigint,delta int,quantity_after int,action text,reason text,id bigint);
 CREATE TEMP TABLE business_order_notifications(order_id bigint,destination text,embed jsonb,delivered_message_id text,next_attempt_at timestamptz NOT NULL DEFAULT NOW(),PRIMARY KEY(order_id,destination));
 INSERT INTO money_transactions VALUES(1,'office','credit',200,1000,1200,'Order completion',1,42);
 INSERT INTO business_order_items VALUES(1,42);
 INSERT INTO safebox_stock_items VALUES(1,'Vector');
 INSERT INTO safebox_stock_transactions VALUES(1,1,-2,8,'withdraw','Order completion',1);`)
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d := &orderDraft{OrderID: 42, Total: 200, UpdatedAt: time.Now(), UpdatedBy: "Admin"}
	tx, err := fixture.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.queueOrderMovement(ctx, tx, d, "member", "office"); err != nil {
		t.Fatal(err)
	}
	var officeOnly bool
	if err = tx.QueryRow(ctx, `SELECT COUNT(*)=1 AND MIN(destination)='office' FROM business_order_notifications`).Scan(&officeOnly); err != nil || !officeOnly {
		t.Fatalf("office stage queued unrelated destination: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err = fixture.QueryRow(ctx, `SELECT COUNT(*) FROM business_order_notifications`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback left notifications: %d %v", count, err)
	}
	tx, err = fixture.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = b.queueOrderMovement(ctx, tx, d, "member", "boss"); err != nil {
			t.Fatal(err)
		}
		if err = b.queueOrderMovement(ctx, tx, d, "member", "office"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = fixture.QueryRow(ctx, `SELECT COUNT(*) FROM business_order_notifications WHERE embed->>'timestamp' IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected one snapshot per destination: %d %v", count, err)
	}
	// Unconfigured channels must remain pending rather than lose a notification.
	found, err := b.deliverOrderNotification(ctx, fixture, nil)
	if err != nil || !found {
		t.Fatalf("pending delivery: %v %v", found, err)
	}
	if err = fixture.QueryRow(ctx, `SELECT COUNT(*) FROM business_order_notifications WHERE delivered_message_id IS NULL AND next_attempt_at>NOW()`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failure not scheduled: %d %v", count, err)
	}
}
