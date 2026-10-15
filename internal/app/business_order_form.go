package app

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func isOrderFormInteraction(i *discordgo.Interaction) bool {
	if i.Type == discordgo.InteractionMessageComponent {
		return strings.HasPrefix(i.MessageComponentData().CustomID, "orderform:")
	}
	if i.Type == discordgo.InteractionModalSubmit {
		return strings.HasPrefix(i.ModalSubmitData().CustomID, "orderform:")
	}
	return false
}

func orderFormComponents(key string, d *orderDraft) []discordgo.MessageComponent {
	rows := []discordgo.MessageComponent{}
	menu := func(action, placeholder string, opts []discordgo.SelectMenuOption) {
		if len(opts) == 0 {
			return
		}
		if len(opts) > 25 {
			opts = opts[:25]
		}
		rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{CustomID: "orderform:" + action + ":" + key, Placeholder: placeholder, Options: opts}}})
	}
	opts := []discordgo.SelectMenuOption{}
	for _, e := range d.Catalog {
		if e.Direction == d.Direction && e.PartnerCode == d.Partner {
			opts = append(opts, discordgo.SelectMenuOption{Label: e.Name, Value: strconv.FormatInt(e.ID, 10), Description: fmt.Sprintf("$%d / %s", e.Price, e.Unit)})
		}
	}
	menu("product", "Add or update a product", opts)
	rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{CustomID: "orderform:details:" + key, Label: "Order details", Style: discordgo.SecondaryButton},
		discordgo.Button{CustomID: "order:" + key, Label: "Submit", Style: discordgo.SuccessButton, Disabled: len(d.Lines) == 0 || d.Partner == ""},
		discordgo.Button{CustomID: "orderform:clear:" + key, Label: "Clear items", Style: discordgo.DangerButton, Disabled: len(d.Lines) == 0},
	}})
	return rows
}
func (b *Bot) renderOrderForm(s *discordgo.Session, i *discordgo.Interaction, key string, d *orderDraft, feedback string) {
	e := orderEmbed(d)
	e.Title = "Order"
	if d.Direction == "purchase" {
		e.Title = "Purchase"
	}
	e.Description = "Select product → enter quantity. Use Order details for dates and extra costs."
	e.Footer = &discordgo.MessageEmbedFooter{Text: "Draft expires after 15 minutes · Quantity 0 removes a product"}
	b.orderReply(s, i, feedback, e, orderFormComponents(key, d))
}
func orderText(id, label, value string, required bool, max int) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: id, Label: label, Style: discordgo.TextInputShort, Value: value, Required: required, MaxLength: max}}}
}
func recalculateOrder(d *orderDraft) error {
	// Validate subtotal through the existing overflow checks before applying fees.
	if len(d.Lines) > 0 && d.AdditionalInput != "" {
		parts := []string{}
		for _, line := range d.Lines {
			parts = append(parts, fmt.Sprintf("%d:%d", line.Entry.ID, line.Quantity))
		}
		base, err := buildOrderDraft(d.Catalog, d.Direction, d.Partner, strings.Join(parts, ","), d.Date.Format("2006-01-02"), d.Pickup.Format("2006-01-02"), d.Notes, 0)
		if err != nil {
			return err
		}
		d.Additional, err = parseOrderAdditional(d.AdditionalInput, base.Total)
		if err != nil {
			return err
		}
	}

	if len(d.Lines) == 0 {
		if strings.HasSuffix(d.AdditionalInput, "%") {
			d.Additional = 0
		}
		d.Total = 0
		return nil
	}
	parts := []string{}
	for _, line := range d.Lines {
		parts = append(parts, fmt.Sprintf("%d:%d", line.Entry.ID, line.Quantity))
	}
	calculated, err := buildOrderDraft(d.Catalog, d.Direction, d.Partner, strings.Join(parts, ","), d.Date.Format("2006-01-02"), d.Pickup.Format("2006-01-02"), d.Notes, d.Additional)
	if err != nil {
		return err
	}
	d.Lines, d.Total = calculated.Lines, calculated.Total
	return nil
}
func (b *Bot) handleOrderForm(s *discordgo.Session, i *discordgo.Interaction) {
	custom := ""
	values := map[string]string{}
	if i.Type == discordgo.InteractionMessageComponent {
		custom = i.MessageComponentData().CustomID
	} else {
		data := i.ModalSubmitData()
		custom = data.CustomID
		for _, c := range data.Components {
			row, ok := c.(*discordgo.ActionsRow)
			if !ok {
				continue
			}
			for _, field := range row.Components {
				if input, ok := field.(*discordgo.TextInput); ok {
					values[input.CustomID] = input.Value
				}
			}
		}
	}
	parts := strings.Split(custom, ":")
	if len(parts) < 3 {
		return
	}
	action, key := parts[1], parts[2]
	uid, err := interactionUserID(i)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	member, err := b.members.FindByDiscordUserID(ctx, uid)
	d := b.lockOrderDraft(key)
	if d != nil {
		defer d.mu.Unlock()
	}
	valid := err == nil && member.IsAdmin && d != nil && !d.Closed && d.UserID == uid && d.ChannelID == i.ChannelID && b.inBusinessOrderChannel(i.ChannelID) && d.OrderID == 0 && time.Now().Before(d.Expires)
	if !valid {
		_ = s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: "Order draft expired or unavailable. Run /order again.", Flags: discordgo.MessageFlagsEphemeral}})
		return
	}
	if i.Type == discordgo.InteractionMessageComponent && (action == "product" || action == "details") {
		modal := &discordgo.InteractionResponseData{CustomID: custom, Title: "Order details"}
		if action == "product" {
			selected := i.MessageComponentData().Values
			if len(selected) != 1 {
				return
			}
			id, parseErr := strconv.ParseInt(selected[0], 10, 64)
			if parseErr != nil {
				return
			}
			found := false
			for _, e := range d.Catalog {
				if e.ID == id && e.Direction == d.Direction && e.PartnerCode == d.Partner {
					found = true
					break
				}
			}
			if !found {
				return
			}
			qty := "1"
			for _, line := range d.Lines {
				if line.Entry.ID == id {
					qty = strconv.FormatInt(line.Quantity, 10)
				}
			}
			modal.CustomID = custom + ":" + selected[0]
			modal.Title = "Product quantity"
			modal.Components = []discordgo.MessageComponent{orderText("quantity", "Quantity (0 removes product)", qty, true, 10)}
		} else {
			modal.Components = []discordgo.MessageComponent{orderText("order_date", "Order date YYYY-MM-DD", d.Date.Format("2006-01-02"), true, 10), orderText("pickup_date", "Pickup date YYYY-MM-DD", d.Pickup.Format("2006-01-02"), true, 10), orderText("additional_price", "Extra cost: +15000, +15%, or -10%", orderAdditionalInput(d), true, 16), orderText("notes", "Notes", d.Notes, false, 1000)}
		}
		if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: modal}); err != nil {
			b.logger.Error("open order modal", "error", err)
		}
		return
	}
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
		return
	}
	next := *d
	next.Lines = append([]orderLine(nil), d.Lines...)
	feedback := ""
	switch action {
	case "clear":
		next.Lines = nil
		next.Total = 0
	case "details":
		next.Date, err = time.Parse("2006-01-02", values["order_date"])
		if err == nil {
			next.Pickup, err = time.Parse("2006-01-02", values["pickup_date"])
			if err == nil && !next.Date.Equal(d.Date) && next.Pickup.Equal(d.Pickup) && d.Pickup.Equal(defaultOrderPickup(d.Direction, d.Date)) {
				next.Pickup = defaultOrderPickup(d.Direction, next.Date)
			}
		}
		if err == nil {
			next.AdditionalInput = strings.TrimSpace(values["additional_price"])
			next.Additional, err = parseOrderAdditional(next.AdditionalInput, 0)
		}
		next.Notes = strings.TrimSpace(values["notes"])
		if err == nil && (next.Additional != 0 || strings.Contains(next.AdditionalInput, "%")) && next.Notes == "" {
			err = fmt.Errorf("Notes required for additional price.")
		}
	case "product":
		if len(parts) != 4 {
			return
		}
		var id, qty int64
		id, err = strconv.ParseInt(parts[3], 10, 64)
		if err == nil {
			qty, err = strconv.ParseInt(strings.TrimSpace(values["quantity"]), 10, 64)
		}
		if err == nil && qty < 0 {
			err = fmt.Errorf("Quantity must be zero or positive.")
		}
		if err == nil {
			kept := next.Lines[:0]
			for _, line := range next.Lines {
				if line.Entry.ID != id {
					kept = append(kept, line)
				}
			}
			next.Lines = kept
			if qty > 0 {
				found := false
				for _, e := range d.Catalog {
					if e.ID == id && e.Direction == d.Direction && e.PartnerCode == d.Partner {
						next.Lines = append(next.Lines, orderLine{Entry: e, Quantity: qty})
						found = true
						break
					}
				}
				if !found {
					err = fmt.Errorf("Product unavailable for selected partner.")
				}
			}
		}
	default:
		return
	}
	if err == nil {
		err = recalculateOrder(&next)
	}
	if err != nil {
		feedback = "Invalid form: " + err.Error()
	} else {
		next.Expires = time.Now().Add(15 * time.Minute)
		*d = next
	}
	b.renderOrderForm(s, i, key, d, feedback)
}

var orderPercentagePattern = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]{1,2})?%$`)

func orderAdditionalInput(d *orderDraft) string {
	if d.AdditionalInput != "" {
		return d.AdditionalInput
	}
	return strconv.FormatInt(d.Additional, 10)
}
func parseOrderAdditional(raw string, subtotal int64) (int64, error) {
	raw = strings.TrimSpace(raw)
	invalid := fmt.Errorf("Use a whole amount (e.g. +15000) or percentage (e.g. +15%% or -10%%).")
	if !strings.HasSuffix(raw, "%") {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, invalid
		}
		return value, nil
	}
	if !orderPercentagePattern.MatchString(raw) {
		return 0, invalid
	}
	rate, ok := new(big.Rat).SetString(strings.TrimSuffix(raw, "%"))
	if !ok {
		return 0, invalid
	}
	amount := new(big.Rat).Mul(rate, new(big.Rat).SetInt64(subtotal))
	amount.Quo(amount, big.NewRat(100, 1))
	// Round half away from zero to whole currency units without floating point.
	sign := amount.Sign()
	num := new(big.Int).Abs(amount.Num())
	den := amount.Denom()
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(num, den, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(den) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if sign < 0 {
		quotient.Neg(quotient)
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("Additional price exceeds supported range.")
	}
	return quotient.Int64(), nil
}
