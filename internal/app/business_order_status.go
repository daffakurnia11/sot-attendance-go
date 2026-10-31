package app

import (
	"context"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"strconv"
	"strings"
	"time"
)

func orderStatuses(direction string) []string {
	if direction == "sale" {
		return []string{"open", "ready", "complete", "cancelled"}
	}
	if direction == "purchase" {
		return []string{"open", "paid", "complete", "cancelled"}
	}
	return nil
}
func orderStatusComponents(d *orderDraft) []discordgo.MessageComponent {
	if d.Status == "cancelled" || d.Status == "complete" {
		return []discordgo.MessageComponent{}
	}
	buttons := []discordgo.MessageComponent{discordgo.Button{CustomID: fmt.Sprintf("orderstatus:%d:cancelled", d.OrderID), Label: "Cancel", Style: discordgo.DangerButton}}
	next := "complete"
	if d.Status == "open" || d.Status == "" {
		next = "ready"
		if d.Direction == "purchase" {
			next = "paid"
		}
	}
	if d.Status != "complete" {
		buttons = append(buttons, discordgo.Button{CustomID: fmt.Sprintf("orderstatus:%d:%s", d.OrderID, next), Label: orderStatusLabel(next), Style: discordgo.SuccessButton})
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}
func (b *Bot) handleOrderUpdate(s *discordgo.Session, i *discordgo.Interaction) {
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		return
	}
	reply := func(msg string) { b.orderReply(s, i, msg, nil, []discordgo.MessageComponent{}) }
	uid, err := interactionUserID(i)
	if err != nil {
		reply("Guild member required.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := b.members.FindByDiscordUserID(ctx, uid)
	if err != nil || !m.IsAdmin {
		reply("Administrator permission required.")
		return
	}
	var id int64
	status := ""
	movement := ""
	if i.Type == discordgo.InteractionApplicationCommand {
		opts := i.ApplicationCommandData().Options
		if len(opts) != 1 || opts[0].Name != "order_id" {
			reply("Order ID required.")
			return
		}
		id = opts[0].IntValue()
	} else {
		data := i.MessageComponentData()
		parts := strings.Split(strings.TrimPrefix(data.CustomID, "orderstatus:"), ":")
		id, err = strconv.ParseInt(parts[0], 10, 64)
		if len(parts) == 2 {
			status = parts[1]
		} else if len(parts) == 1 && len(data.Values) == 1 {
			status = data.Values[0]
		}
		if err != nil || status == "" {
			reply("Invalid status action.")
			return
		}
	}
	if id <= 0 {
		reply("Invalid order ID.")
		return
	}
	tx, err := b.database.Begin(ctx)
	if err != nil {
		reply("Could not load order.")
		return
	}
	defer tx.Rollback(ctx)
	d := &orderDraft{OrderID: id}
	var messageID *string
	err = tx.QueryRow(ctx, `SELECT o.direction,p.code,p.name,o.order_date,o.pickup_date,o.additional_price,o.total_price,o.notes,o.status,o.discord_message_id,o.created_at,o.updated_at,COALESCE(NULLIF(u.display_name,''),u.username,'Unknown'),COALESCE(buyer.discord_user_id,'') FROM business_orders o JOIN business_partners p ON p.id=o.partner_id LEFT JOIN members u ON u.id=o.updated_by_member_id LEFT JOIN members buyer ON buyer.id=o.buyer_member_id WHERE o.id=$1 FOR UPDATE OF o`, id).Scan(&d.Direction, &d.Partner, &d.PartnerName, &d.Date, &d.Pickup, &d.Additional, &d.Total, &d.Notes, &d.Status, &messageID, &d.CreatedAt, &d.UpdatedAt, &d.UpdatedBy, &d.BuyerDiscordID)
	if err != nil {
		b.logger.Warn("load order for status", "order_id", id, "error", err)
		reply("Order unavailable.")
		return
	}
	if !b.inBusinessOrderChannel(i.ChannelID) {
		reply("Use the configured business order-list channel.")
		return
	}
	if status != "" {
		valid := false
		for _, allowed := range orderStatuses(d.Direction) {
			if status == allowed {
				valid = true
			}
		}
		if !valid {
			reply("Status unavailable for this order direction.")
			return
		}
		if d.Status == "complete" {
			reply("Order already Complete. Money and stock cannot be settled again or reopened here.")
			return
		}
		movement, err = business.SettleOrderStatus(ctx, tx, id, m.ID, "boss", status)
		if err != nil {
			b.logger.Error("settle business order stage", "order_id", id, "status", status, "member_id", m.ID, "error", err)
			reply("Could not change status. Check required stage, office funds, Boss Safebox stock, and existing ledger entries. No changes saved.")
			return
		}

		if err = tx.QueryRow(ctx, `UPDATE business_orders SET status=$2,updated_at=NOW(),updated_by_member_id=$3 WHERE id=$1 RETURNING updated_at`, id, status, m.ID).Scan(&d.UpdatedAt); err != nil {
			reply("Could not update order.")
			return
		}
		d.Status = status
		d.UpdatedBy = m.DisplayName
		if d.UpdatedBy == "" {
			d.UpdatedBy = m.Username
		}
	}
	rows, err := tx.Query(ctx, `SELECT product_name,unit,unit_price,quantity,stock_units_per_unit FROM business_order_items WHERE order_id=$1 ORDER BY id`, id)
	if err != nil {
		reply("Could not load order products.")
		return
	}
	for rows.Next() {
		var line orderLine
		if err = rows.Scan(&line.Entry.Name, &line.Entry.Unit, &line.Entry.Price, &line.Quantity, &line.Conversion); err != nil {
			break
		}
		d.Lines = append(d.Lines, line)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		reply("Could not read order products.")
		return
	}
	if movement != "" {
		if err = b.queueOrderMovement(ctx, tx, d, uid, movement); err != nil {
			b.logger.Error("queue completed order ledgers", "order_id", id, "error", err)
			reply("Could not prepare ledger notifications. No changes saved.")
			return
		}
	}
	if err = tx.Commit(ctx); err != nil {
		b.logger.Error("commit order status", "order_id", id, "error", err)
		reply("Could not confirm status update. Reload order before retrying.")
		return
	}
	if status != "" {
		b.logger.Info("business order status updated", "order_id", id, "status", status, "member_id", m.ID)
		if messageID != nil {
			embeds := []*discordgo.MessageEmbed{orderEmbed(d)}
			components := orderStatusComponents(d)
			if _, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: *messageID, Channel: i.ChannelID, Embeds: &embeds, Components: &components}); err != nil {
				b.logger.Error("refresh order message", "order_id", id, "error", err)
				b.orderReply(s, i, "Status saved; channel message refresh failed. Controls below remain available.", orderEmbed(d), orderStatusComponents(d))
				return
			}
		}
	}
	if status != "" {
		confirmation := orderStatusConfirmation(d, movement)
		if _, err := s.InteractionResponseEdit(i, confirmation); err != nil {
			b.logger.Error("confirm order status response", "order_id", id, "error", err)
		}
		return
	}
	b.orderReply(s, i, "Sales: Ready withdraws Boss stock, Complete credits office money. Purchases: Paid debits office money, Complete deposits Boss stock. Cancel reverses recorded money and stock movements.", orderEmbed(d), orderStatusComponents(d))
}

func orderStatusConfirmation(d *orderDraft, movement string) *discordgo.WebhookEdit {
	effect := "No new money or stock transactions."
	if movement == "cancel" {
		effect = "Recorded money and stock movements reversed. Reversal notifications queued where applicable."
	}
	if movement == "boss" {
		effect = "Boss Safebox stock updated. Ledger notification queued."
	}
	if movement == "office" {
		effect = "Office money updated. Ledger notification queued."
	}

	content := fmt.Sprintf("Order #%d updated to **%s**. %s", d.OrderID, orderStatusLabel(d.Status), effect)
	embeds := []*discordgo.MessageEmbed{}
	components := []discordgo.MessageComponent{}
	return &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components}
}

func orderStatusLabel(status string) string {
	switch status {
	case "open":
		return "Open"
	case "cancelled":
		return "Cancelled"
	case "ready":
		return "Ready"
	case "paid":
		return "Paid"
	case "complete":
		return "Complete"
	default:
		return status
	}
}
