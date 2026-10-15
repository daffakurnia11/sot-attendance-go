package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	commandmoney "github.com/daffakurniawan/sot-discord-bot/internal/command/money"
	commandstash "github.com/daffakurniawan/sot-discord-bot/internal/command/stash"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
	"github.com/jackc/pgx/v5"
)

// Queue ledger snapshots in the settlement transaction; delivery never replays balances.
func (b *Bot) queueOrderMovement(ctx context.Context, tx pgx.Tx, d *orderDraft, actorDiscordID, destination string) error {
	var failures []error
	send := func(destination string, embed *discordgo.MessageEmbed) {
		payload, err := json.Marshal(embed)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO business_order_notifications(order_id,destination,embed) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, d.OrderID, destination, payload)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	if destination == "office" {
		var t moneydomain.Transaction
		err := tx.QueryRow(ctx, `SELECT id,account_type,CASE WHEN direction='credit' THEN 'deposit' ELSE 'withdraw' END,amount,balance_before,balance_after,reason,actor_member_id FROM money_transactions WHERE business_order_id=$1 ORDER BY id LIMIT 1`, d.OrderID).Scan(&t.ID, &t.Account, &t.Action, &t.Amount, &t.BalanceBefore, &t.BalanceAfter, &t.Reason, &t.ActorMemberID)
		if err != nil {
			failures = append(failures, err)
		} else {
			embed := commandmoney.TransactionEmbed(t, d.UpdatedBy)
			embed.Timestamp = d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
			send("office", embed)
		}
		return errors.Join(failures...)
	}
	if destination != "boss" {
		return fmt.Errorf("invalid ledger destination %q", destination)
	}
	rows, err := tx.Query(ctx, `SELECT i.name,abs(t.delta),t.quantity_after,t.action,t.reason FROM safebox_stock_transactions t JOIN business_order_items o ON o.id=t.business_order_item_id JOIN safebox_stock_items i ON i.id=t.item_id WHERE o.order_id=$1 ORDER BY t.id`, d.OrderID)
	if err != nil {
		return fmt.Errorf("read order stock ledger: %w", err)
	}
	lines := []commandstash.Line{}
	var action, reason string
	for rows.Next() {
		var line commandstash.Line
		if err = rows.Scan(&line.Name, &line.Quantity, &line.After, &action, &reason); err != nil {
			break
		}
		lines = append(lines, line)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		failures = append(failures, fmt.Errorf("read order stock entries: %v %v", err, rowErr))
	} else if len(lines) > 0 {
		embed := commandstash.TransactionEmbed("boss", action, lines, d.UpdatedBy, actorDiscordID, reason)
		embed.Timestamp = d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
		send("boss", embed)
	} else {
		failures = append(failures, fmt.Errorf("order stage has no stock ledger entries"))
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d order ledger notification failures: %v", len(failures), failures)
	}
	return nil
}

func (b *Bot) runOrderNotifications(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		b.flushOrderNotifications(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (b *Bot) flushOrderNotifications(ctx context.Context) {
	for n := 0; n < 20 && ctx.Err() == nil; n++ {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		tx, err := b.database.Begin(attempt)
		if err != nil {
			cancel()
			b.logger.Error("begin order notification delivery", "error", err)
			return
		}
		found, err := b.deliverOrderNotification(attempt, tx, b.session)
		if err == nil {
			err = tx.Commit(attempt)
		}
		_ = tx.Rollback(context.Background())
		cancel()
		if err != nil {
			b.logger.Error("deliver order notification", "error", err)
			return
		}
		if !found {
			return
		}
	}
}
func (b *Bot) deliverOrderNotification(ctx context.Context, tx pgx.Tx, s *discordgo.Session) (bool, error) {
	var id int64
	var destination string
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT order_id,destination,embed FROM business_order_notifications WHERE delivered_message_id IS NULL AND next_attempt_at<=NOW() ORDER BY next_attempt_at,order_id,destination FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &destination, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	channel := b.officeMoneyChannelID
	if destination == "boss" {
		channel = b.stashChannels.boss
	}
	var embed discordgo.MessageEmbed
	err = json.Unmarshal(payload, &embed)
	var message *discordgo.Message
	if err == nil && channel == "" {
		err = fmt.Errorf("notification channel %s not configured", destination)
	}
	// shortcut: delivery is at least once across an accepted send followed by a crash; add Discord deduplication if duplicate embeds become unacceptable.
	if err == nil {
		message, err = s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{&embed}, AllowedMentions: &discordgo.MessageAllowedMentions{}}, discordgo.WithContext(ctx))
	}
	if err != nil {
		b.logger.Error("post order ledger notification; retry queued", "order_id", id, "destination", destination, "error", err)
		_, saveErr := tx.Exec(ctx, `UPDATE business_order_notifications SET next_attempt_at=NOW()+INTERVAL '1 minute' WHERE order_id=$1 AND destination=$2`, id, destination)
		return true, saveErr
	}
	_, err = tx.Exec(ctx, `UPDATE business_order_notifications SET delivered_message_id=$3 WHERE order_id=$1 AND destination=$2`, id, destination, message.ID)
	return true, err
}
