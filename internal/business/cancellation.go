package business

import (
	"context"
	"fmt"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
	"github.com/jackc/pgx/v5"
	"math"
)

// Caller holds the order lock. Compensating entries preserve the original audit trail.
func reverseOrder(ctx context.Context, tx pgx.Tx, id, actor int64, safebox string) (string, error) {
	reason := fmt.Sprintf("Business order #%d cancelled", id)
	var net int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(CASE WHEN direction='credit' THEN amount ELSE -amount END),0)::bigint FROM money_transactions WHERE business_order_id=$1`, id).Scan(&net); err != nil {
		return "", err
	}
	if net != 0 {
		action := moneydomain.ActionDeposit
		amount := net
		if net > 0 {
			action = moneydomain.ActionWithdraw
		} else {
			if net == math.MinInt64 {
				return "", fmt.Errorf("money reversal exceeds supported range")
			}
			amount = -net
		}
		t, err := moneydomain.NewRepository(tx).Transact(ctx, moneydomain.Transaction{Account: moneydomain.AccountOffice, Action: action, Amount: amount, Reason: reason, ActorMemberID: actor})
		if err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `UPDATE money_transactions SET business_order_id=$2 WHERE id=$1`, t.ID, id); err != nil {
			return "", err
		}
	}
	rows, err := tx.Query(ctx, `SELECT t.business_order_item_id,t.item_id,SUM(t.delta)::bigint FROM safebox_stock_transactions t JOIN business_order_items i ON i.id=t.business_order_item_id WHERE i.order_id=$1 AND t.safebox=$2 GROUP BY t.business_order_item_id,t.item_id ORDER BY t.item_id,t.business_order_item_id`, id, safebox)
	if err != nil {
		return "", err
	}
	type movement struct{ line, item, delta int64 }
	moves := []movement{}
	for rows.Next() {
		var m movement
		if err = rows.Scan(&m.line, &m.item, &m.delta); err != nil {
			break
		}
		moves = append(moves, m)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if rowErr != nil {
		return "", rowErr
	}
	for _, m := range moves {
		if m.delta == 0 {
			continue
		}
		if m.delta > math.MaxInt32 || m.delta < -math.MaxInt32 {
			return "", fmt.Errorf("stock reversal exceeds supported range")
		}
		var before int64
		if err = tx.QueryRow(ctx, `SELECT quantity FROM safebox_stock_balances WHERE safebox=$1 AND item_id=$2 FOR UPDATE`, safebox, m.item).Scan(&before); err != nil {
			return "", err
		}
		after := before - m.delta
		if after < 0 || after > math.MaxInt32 {
			return "", fmt.Errorf("insufficient stock or reversal overflow for item %d", m.item)
		}
		action := "deposit"
		if m.delta > 0 {
			action = "withdraw"
		}
		if _, err = tx.Exec(ctx, `UPDATE safebox_stock_balances SET quantity=$3,updated_at=NOW() WHERE safebox=$1 AND item_id=$2`, safebox, m.item, after); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO safebox_stock_transactions(safebox,item_id,action,quantity_before,quantity_after,delta,reason,actor_member_id,business_order_item_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, safebox, m.item, action, before, after, -m.delta, reason, actor, m.line); err != nil {
			return "", err
		}
	}
	return "cancel", nil
}
