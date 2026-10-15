package business

import (
	"context"
	"errors"
	"fmt"
	"math"

	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
	"github.com/jackc/pgx/v5"
)

// SettleOrderStatus runs in the status transaction. Each stage posts one ledger only.
func SettleOrderStatus(ctx context.Context, tx pgx.Tx, orderID, actor int64, safebox, target string) (string, error) {
	if safebox != "boss" {
		return "", errors.New("Business orders require Boss Safebox.")
	}
	var direction, status string
	var total int64
	if err := tx.QueryRow(ctx, `SELECT direction,status,total_price FROM business_orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&direction, &status, &total); err != nil {
		return "", err
	}
	destination, err := OrderStatusMovement(direction, status, target)
	if err != nil || destination == "" {
		return "", err
	}
	var moneyPosted, stockPosted bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM money_transactions WHERE business_order_id=$1), EXISTS(SELECT 1 FROM safebox_stock_transactions t JOIN business_order_items i ON i.id=t.business_order_item_id WHERE i.order_id=$1)`, orderID).Scan(&moneyPosted, &stockPosted); err != nil {
		return "", err
	}
	if target == "complete" && ((direction == "sale" && !stockPosted) || (direction == "purchase" && total > 0 && !moneyPosted)) {
		return "", errors.New("Previous stage has no ledger entries. Reconcile historical order before completing.")
	}
	if destination == "office" && moneyPosted || destination == "boss" && stockPosted {
		return "", errors.New("Order already has entries for this ledger; reconcile before changing status.")
	}
	reason := fmt.Sprintf("Business order #%d %s", orderID, target)
	if destination == "office" {
		if total == 0 {
			return "", nil
		}
		action := moneydomain.ActionDeposit
		if direction == "purchase" {
			action = moneydomain.ActionWithdraw
		}
		transaction, err := moneydomain.NewRepository(tx).Transact(ctx, moneydomain.Transaction{Account: moneydomain.AccountOffice, Action: action, Amount: total, Reason: reason, ActorMemberID: actor})
		if err != nil {
			return "", err
		}
		_, err = tx.Exec(ctx, `UPDATE money_transactions SET business_order_id=$2 WHERE id=$1`, transaction.ID, orderID)
		return destination, err
	}
	type line struct{ id, item, qty, conversion int64 }
	rows, err := tx.Query(ctx, `SELECT id,stock_item_id,quantity,stock_units_per_unit FROM business_order_items WHERE order_id=$1 ORDER BY stock_item_id,id`, orderID)
	if err != nil {
		return "", err
	}
	lines := []line{}
	for rows.Next() {
		var l line
		if err = rows.Scan(&l.id, &l.item, &l.qty, &l.conversion); err != nil {
			break
		}
		lines = append(lines, l)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if rowErr != nil {
		return "", rowErr
	}
	if len(lines) == 0 {
		return "", errors.New("Order has no products.")
	}
	for _, l := range lines {
		if l.qty <= 0 || l.conversion <= 0 || l.qty > math.MaxInt32/l.conversion {
			return "", errors.New("Stock movement exceeds supported range.")
		}
		var before int64
		if err = tx.QueryRow(ctx, `SELECT quantity FROM safebox_stock_balances WHERE safebox=$1 AND item_id=$2 FOR UPDATE`, safebox, l.item).Scan(&before); err != nil {
			return "", fmt.Errorf("load safebox item %d: %w", l.item, err)
		}
		delta := l.qty * l.conversion
		action := "deposit"
		if direction == "sale" {
			delta = -delta
			action = "withdraw"
		}
		after := before + delta
		if after < 0 {
			return "", fmt.Errorf("Insufficient %s safebox stock for item %d.", safebox, l.item)
		}
		if after > math.MaxInt32 {
			return "", errors.New("Safebox stock exceeds supported range.")
		}
		if _, err = tx.Exec(ctx, `UPDATE safebox_stock_balances SET quantity=$3,updated_at=NOW() WHERE safebox=$1 AND item_id=$2`, safebox, l.item, after); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO safebox_stock_transactions(safebox,item_id,action,quantity_before,quantity_after,delta,reason,actor_member_id,business_order_item_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, safebox, l.item, action, before, after, delta, reason, actor, l.id); err != nil {
			return "", err
		}
	}
	return destination, nil
}

// Stages are forward-only because ledger movements cannot be undone by a label change.
func OrderStatusMovement(direction, current, target string) (string, error) {
	stage := "ready"
	if direction == "purchase" {
		stage = "paid"
	} else if direction != "sale" {
		return "", errors.New("Invalid order direction.")
	}
	if current == target && (current == "open" || current == stage || current == "complete") {
		return "", nil
	}
	if current == "open" && target == stage {
		if direction == "sale" {
			return "boss", nil
		}
		return "office", nil
	}
	if current == stage && target == "complete" {
		if direction == "sale" {
			return "office", nil
		}
		return "boss", nil
	}
	return "", fmt.Errorf("Status must advance Open → %s → Complete; ledger movements cannot be reversed here.", stage)
}
