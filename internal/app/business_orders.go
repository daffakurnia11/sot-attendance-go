package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"github.com/jackc/pgx/v5"
)

type orderLine struct {
	Entry                business.Entry
	Quantity, Conversion int64
}
type orderDraft struct {
	mu                                           *sync.Mutex
	Closed                                       bool
	BuyerMemberID                                int64
	BuyerDiscordID                               string
	AdditionalInput                              string
	UserID, ChannelID, Direction, Partner, Notes string
	Date, Pickup                                 time.Time
	Additional, Total, OrderID                   int64
	Lines                                        []orderLine
	Catalog                                      []business.Entry
	Expires                                      time.Time
	Status                                       string
	CreatedAt, UpdatedAt                         time.Time
	UpdatedBy                                    string
}

func orderSlashCommand(contexts []discordgo.InteractionContextType) *discordgo.ApplicationCommand {
	return businessSlashCommand("order", "Create a sale order", contexts)
}
func businessSlashCommand(name, description string, contexts []discordgo.InteractionContextType) *discordgo.ApplicationCommand {
	partnerDescription := "Partner code: BZ, CN, CH, DTM, DVG, DW, GWF, NOCTIS"
	if name == "purchase" {
		partnerDescription = "Partner code: BZ, CH, DTM, DVG, DW, GWF, NOCTIS"
	}
	options := []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "partner", Description: partnerDescription, Required: name == "purchase", MaxLength: 50}}
	if name == "order" {
		options = append(options, &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionUser, Name: "member", Description: "Member buying from SOT internal catalog (choose member OR partner)"})
	}
	return &discordgo.ApplicationCommand{Name: name, Description: description, Contexts: &contexts, Options: options}
}

func (b *Bot) inBusinessOrderChannel(channel string) bool {
	return channel != "" && channel == b.businessSaleChannelID
}

func orderCommandDirection(action string) string {
	switch action {
	case "purchase":
		return "purchase"
	case "order":
		return "sale"
	default:
		return ""
	}
}

func buildOrderDraft(entries []business.Entry, direction, partner, items, date, pickup, notes string, additional int64) (*orderDraft, error) {
	d := &orderDraft{Direction: direction, Partner: strings.ToUpper(strings.TrimSpace(partner)), Notes: strings.TrimSpace(notes), Additional: additional}
	var err error
	d.Date, err = time.Parse("2006-01-02", date)
	if err != nil {
		return nil, errors.New("Order date must be YYYY-MM-DD.")
	}
	d.Pickup, err = time.Parse("2006-01-02", pickup)
	if err != nil {
		return nil, errors.New("Pickup date must be YYYY-MM-DD.")
	}
	if additional != 0 && d.Notes == "" {
		return nil, errors.New("Notes required for additional price.")
	}
	parts := strings.Split(items, ",")
	if len(parts) > 20 {
		return nil, errors.New("Maximum 20 products per order.")
	}
	seen := map[int64]bool{}
	for _, part := range parts {
		pair := strings.Split(strings.TrimSpace(part), ":")
		if len(pair) != 2 {
			return nil, errors.New("Items must use ID:quantity, separated by commas.")
		}
		id, e1 := strconv.ParseInt(strings.TrimSpace(pair[0]), 10, 64)
		qty, e2 := strconv.ParseInt(strings.TrimSpace(pair[1]), 10, 64)
		if e1 != nil || e2 != nil || qty <= 0 || seen[id] {
			return nil, errors.New("Invalid or duplicate product ID/quantity.")
		}
		seen[id] = true
		var selected *business.Entry
		for i := range entries {
			e := &entries[i]
			if e.ID == id && e.Direction == direction && strings.EqualFold(e.PartnerCode, d.Partner) {
				selected = e
				break
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("Product %d unavailable for this partner/direction.", id)
		}
		conversion := int64(1)
		if selected.Unit == "package" && selected.PartnerCode == "SOT" && direction == "sale" {
			if len(selected.Components) == 0 {
				return nil, errors.New("Package stock components unavailable.")
			}
			for _, component := range selected.Components {
				if component.StockItemID <= 0 || component.Quantity <= 0 || qty > math.MaxInt32/component.Quantity {
					return nil, errors.New("Package quantity exceeds supported stock range.")
				}
			}
		} else if selected.Unit != "piece" {
			if selected.Unit != "clip" {
				return nil, errors.New("Product unit has no supported stock conversion.")
			}
			switch selected.StockItemName {
			case "Ammo Pistol":
				conversion = 80
			case "Ammo SMG":
				conversion = 150
			case "Ammo Rifle":
				conversion = 180
			default:
				return nil, errors.New("Clip conversion unavailable for this product.")
			}
		}
		if selected.Price < 0 || selected.Price > 0 && qty > math.MaxInt64/selected.Price || qty > math.MaxInt32/conversion {
			return nil, errors.New("Quantity exceeds supported range.")
		}
		amount := qty * selected.Price
		if d.Total > math.MaxInt64-amount {
			return nil, errors.New("Order total exceeds supported range.")
		}
		d.Total += amount
		d.Lines = append(d.Lines, orderLine{*selected, qty, conversion})
	}
	if additional > 0 && d.Total > math.MaxInt64-additional || additional < 0 && additional < -d.Total {
		return nil, errors.New("Additional price produces invalid total.")
	}
	d.Total += additional
	return d, nil
}

func orderEmbed(d *orderDraft) *discordgo.MessageEmbed {
	label := "Order"
	if d.Direction == "purchase" {
		label = "Purchase"
	}
	title := label + " preview"
	if d.OrderID > 0 {
		status := d.Status
		if status == "" {
			status = "open"
		}
		title = fmt.Sprintf("%s #%d · %s", label, d.OrderID, orderStatusLabel(status))
	}
	subtotal := int64(0)
	lines := make([]string, 0, len(d.Lines))
	for _, line := range d.Lines {
		subtotal += line.Quantity * line.Entry.Price
		lines = append(lines, fmt.Sprintf("**%s** × %d %s — %s", line.Entry.Name, line.Quantity, line.Entry.Unit, orderMoney(line.Quantity*line.Entry.Price)))
	}
	products := "No products added."
	if len(lines) > 0 {
		products = strings.Join(lines, "\n")
	}
	color := 0x57F287
	if d.Partner == "SOT" && d.Direction == "sale" {
		color = 0x3498DB
	}
	if d.Direction == "purchase" {
		color = 0xED4245
	}
	fields := []*discordgo.MessageEmbedField{
		{Name: "Partner", Value: d.Partner},
		{Name: "Order date", Value: d.Date.Format("02 Jan 2006"), Inline: true},
		{Name: "Pickup date", Value: d.Pickup.Format("02 Jan 2006"), Inline: true},
		{Name: "Products", Value: products},
		{Name: "Price summary", Value: fmt.Sprintf("Subtotal: %s\nAdditional: %s\n**Total: %s**", orderMoney(subtotal), orderMoney(d.Additional), orderMoney(d.Total))},
	}
	if d.BuyerDiscordID != "" {
		fields = append([]*discordgo.MessageEmbedField{{Name: "Buyer", Value: "<@" + d.BuyerDiscordID + ">"}}, fields...)
	}
	if d.Notes != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Notes", Value: d.Notes})
	}
	embed := &discordgo.MessageEmbed{Title: title, Color: color, Fields: fields}
	if !d.CreatedAt.IsZero() {
		zone := time.FixedZone("WIB", 7*60*60)
		updater := d.UpdatedBy
		if updater == "" {
			updater = "Unknown"
		}
		embed.Footer = &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Created: %s · Updated: %s · Last updated by: %s", d.CreatedAt.In(zone).Format("02 Jan 2006 15:04:05 WIB"), d.UpdatedAt.In(zone).Format("02 Jan 2006 15:04:05 WIB"), updater)}
	}
	return embed

}

func (b *Bot) orderReply(s *discordgo.Session, i *discordgo.Interaction, content string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) {
	edit := &discordgo.WebhookEdit{Content: &content, Components: &components}
	if embed != nil {
		edit.Embeds = &[]*discordgo.MessageEmbed{embed}
	}
	if _, err := s.InteractionResponseEdit(i, edit); err != nil {
		b.logger.Error("respond business order", "error", err)
	}
}

func (b *Bot) handleOrderStart(s *discordgo.Session, i *discordgo.Interaction) {
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		return
	}
	fail := func(msg string) { b.orderReply(s, i, msg, nil, []discordgo.MessageComponent{}) }
	if !b.inBusinessOrderChannel(i.ChannelID) {
		fail("Use order commands in the configured business order-list channel.")
		return
	}
	uid, err := interactionUserID(i)
	if err != nil {
		fail("Guild member required.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m, err := b.members.FindByDiscordUserID(ctx, uid)
	if err != nil || !m.IsAdmin {
		fail("Administrator permission required.")
		return
	}
	direction := orderCommandDirection(i.ApplicationCommandData().Name)
	if direction == "" {
		fail("Use /order or /purchase.")
		return
	}
	entries, err := business.NewRepository(b.database).List(ctx)
	if err != nil {
		b.logger.Error("load business order catalog", "error", err)
		fail("Could not load catalog.")
		return
	}
	entries = orderCatalog(entries)
	options := i.ApplicationCommandData().Options
	rawPartner, buyerDiscord, err := orderTarget(options, direction)
	if err != nil {
		fail(err.Error())
		return
	}
	var buyerMemberID int64
	if buyerDiscord != "" {
		buyer, lookupErr := b.members.FindByDiscordUserID(ctx, buyerDiscord)
		if lookupErr != nil {
			fail("Buyer must be a registered SOT member.")
			return
		}
		buyerMemberID = buyer.ID
	}
	partner, err := orderPartnerCode(entries, direction, rawPartner)
	if err != nil {
		fail(err.Error())
		return
	}

	today, _ := time.Parse("2006-01-02", time.Now().In(b.location).Format("2006-01-02"))
	d := &orderDraft{mu: &sync.Mutex{}, UserID: uid, ChannelID: i.ChannelID, Direction: direction, Partner: partner, BuyerMemberID: buyerMemberID, BuyerDiscordID: buyerDiscord, Date: today, Pickup: defaultOrderPickup(direction, today), Catalog: entries, Expires: time.Now().Add(15 * time.Minute)}
	b.orderMu.Lock()
	if b.orderDrafts == nil {
		b.orderDrafts = map[string]*orderDraft{}
	}
	for key, old := range b.orderDrafts {
		if old.mu.TryLock() {
			if time.Now().After(old.Expires) {
				delete(b.orderDrafts, key)
			}
			old.mu.Unlock()
		}
	}
	d.mu.Lock()
	b.orderDrafts[i.ID] = d
	b.orderMu.Unlock()
	b.renderOrderForm(s, i, i.ID, d, "")
	d.mu.Unlock()
}

func (b *Bot) handleOrderConfirm(s *discordgo.Session, i *discordgo.Interaction) {
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
		return
	}
	fail := func(msg string) { b.orderReply(s, i, msg, nil, []discordgo.MessageComponent{}) }
	uid, err := interactionUserID(i)
	if err != nil {
		fail("Guild member required.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := b.members.FindByDiscordUserID(ctx, uid)
	if err != nil || !m.IsAdmin {
		fail("Administrator permission required.")
		return
	}
	key := strings.TrimPrefix(i.MessageComponentData().CustomID, "order:")
	d := b.lockOrderDraft(key)
	if d == nil {
		fail("Order busy or unavailable. Retry shortly.")
		return
	}
	defer d.mu.Unlock()
	if d.Closed || d.UserID != uid || d.ChannelID != i.ChannelID || !b.inBusinessOrderChannel(i.ChannelID) || time.Now().After(d.Expires) {
		fail("Preview expired or belongs to another member/channel. Run /order again.")
		return
	}
	if d.OrderID == 0 {
		if d.Partner == "" || len(d.Lines) == 0 {
			fail("Select partner and add products first.")
			return
		}
		tx, err := b.database.Begin(ctx)
		if err != nil {
			fail("Could not save order.")
			return
		}
		defer tx.Rollback(ctx)
		d.UpdatedBy = m.DisplayName
		if d.UpdatedBy == "" {
			d.UpdatedBy = m.Username
		}
		err = saveBusinessOrder(ctx, tx, d, m.ID)
		if err != nil {
			b.logger.Error("save business order", "order_id", d.OrderID, "error", err)
			if d.OrderID != 0 {
				b.orderReply(s, i, fmt.Sprintf("Order #%d save unconfirmed. Retry to check this order; do not recreate it.", d.OrderID), nil, orderRetryComponents(key))
			} else {
				fail("Could not save order. Run /order again.")
			}
			return
		}

		b.logger.Info("business order created", "order_id", d.OrderID, "member_id", m.ID, "direction", d.Direction)
	}
	// Persist before posting. A failed post leaves the saved order available for retry.
	var messageID *string
	err = b.database.QueryRow(ctx, `SELECT discord_message_id FROM business_orders WHERE id=$1`, d.OrderID).Scan(&messageID)
	if err != nil {
		b.orderReply(s, i, fmt.Sprintf("Order #%d save or message unconfirmed. Retry to check; do not recreate it.", d.OrderID), nil, orderRetryComponents(key))
		return
	}
	if messageID == nil {
		message, sendErr := s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{orderEmbed(d)}, Components: orderStatusComponents(d), AllowedMentions: &discordgo.MessageAllowedMentions{}})
		if sendErr != nil {
			b.logger.Error("post business order", "order_id", d.OrderID, "error", sendErr)
			b.orderReply(s, i, fmt.Sprintf("Order #%d saved. Posting failed; click Create order to retry.", d.OrderID), nil, []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{CustomID: "order:" + key, Label: "Retry posting", Style: discordgo.SuccessButton}}}})
			return
		}
		if _, err = b.database.Exec(ctx, `UPDATE business_orders SET discord_message_id=$2 WHERE id=$1`, d.OrderID, message.ID); err != nil {
			b.logger.Error("link business order message", "order_id", d.OrderID, "message_id", message.ID, "error", err)
			b.deleteOrderDraft(key, d)
			fail(fmt.Sprintf("Order #%d posted; message linkage failed. Do not recreate order.", d.OrderID))
			return
		}
	}
	b.deleteOrderDraft(key, d)
	fail(fmt.Sprintf("Order #%d created. Money and stock unchanged.", d.OrderID))
}

// Internal prices retain their source in the repository and use sale settlement in orders.
func orderCatalog(entries []business.Entry) []business.Entry {
	result := append([]business.Entry(nil), entries...)
	for index := range result {
		if result[index].Direction == "internal" && result[index].PartnerCode == "SOT" {
			result[index].Direction = "sale"
		}
	}
	return result
}

func orderPartnerCode(entries []business.Entry, direction, raw string) (string, error) {
	code := strings.TrimSpace(raw)
	for _, entry := range entries {
		if entry.Direction == direction && strings.EqualFold(entry.PartnerCode, code) {
			return entry.PartnerCode, nil
		}
	}
	return "", errors.New("Unknown partner code or no products available for this command.")
}

func defaultOrderPickup(direction string, date time.Time) time.Time {
	if direction == "sale" {
		return date.AddDate(0, 0, 2)
	}
	return date
}
func orderMoney(value int64) string {
	raw := strconv.FormatInt(value, 10)
	start := 0
	if strings.HasPrefix(raw, "-") {
		start = 1
	}
	for i := len(raw) - 3; i > start; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return "$" + raw
}

// Map lock never spans database or Discord requests. Busy drafts reject concurrent edits.
func (b *Bot) lockOrderDraft(key string) *orderDraft {
	b.orderMu.Lock()
	defer b.orderMu.Unlock()
	d := b.orderDrafts[key]
	if d == nil || !d.mu.TryLock() {
		return nil
	}
	return d
}
func (b *Bot) deleteOrderDraft(key string, d *orderDraft) {
	d.Closed = true
	b.orderMu.Lock()
	delete(b.orderDrafts, key)
	b.orderMu.Unlock()
}
func orderRetryComponents(key string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{CustomID: "order:" + key, Label: "Retry saved order", Style: discordgo.SuccessButton}}}}
}

func saveBusinessOrder(ctx context.Context, tx pgx.Tx, d *orderDraft, memberID int64) error {
	if d.OrderID != 0 {
		return nil
	}

	err := tx.QueryRow(ctx, `INSERT INTO business_orders(partner_id,direction,order_date,pickup_date,additional_price,total_price,notes,created_by_member_id,updated_by_member_id,buyer_member_id) SELECT id,$2,$3,$4,$5,$6,$7,$8,$8,NULLIF($9,0) FROM business_partners WHERE code=$1 RETURNING id,created_at,updated_at`, d.Partner, d.Direction, d.Date, d.Pickup, d.Additional, d.Total, d.Notes, memberID, d.BuyerMemberID).Scan(&d.OrderID, &d.CreatedAt, &d.UpdatedAt)
	if err == nil {
		for _, line := range d.Lines {
			_, err = tx.Exec(ctx, `INSERT INTO business_order_items(order_id,stock_item_id,product_name,unit,unit_price,quantity,stock_units_per_unit) VALUES($1,$2,$3,$4,$5,$6,$7)`, d.OrderID, line.Entry.StockItemID, line.Entry.Name, line.Entry.Unit, line.Entry.Price, line.Quantity, line.Conversion)
			if err == nil && line.Entry.Unit == "package" {
				for _, component := range line.Entry.Components {
					_, err = tx.Exec(ctx, `INSERT INTO business_order_items(order_id,stock_item_id,product_name,unit,unit_price,quantity,stock_units_per_unit) VALUES($1,$2,$3,'piece',0,$4,$5)`, d.OrderID, component.StockItemID, fmt.Sprintf("%s · %d %s per package (included)", line.Entry.Name, component.Quantity, component.Name), line.Quantity, component.Quantity)
					if err != nil {
						break
					}
				}
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		d.OrderID = 0
		return err
	}
	// A commit error may mean the response was lost after the database saved it.
	return tx.Commit(ctx)
}

func orderTarget(options []*discordgo.ApplicationCommandInteractionDataOption, direction string) (string, string, error) {
	partner, buyer := "", ""
	for _, option := range options {
		switch {
		case option.Name == "partner" && option.Type == discordgo.ApplicationCommandOptionString:
			partner = strings.TrimSpace(option.StringValue())
		case option.Name == "member" && option.Type == discordgo.ApplicationCommandOptionUser:
			buyer, _ = option.Value.(string)
		default:
			return "", "", errors.New("Choose partner or member.")
		}
	}
	if buyer != "" && partner == "" && direction == "sale" {
		return "SOT", buyer, nil
	}
	if partner != "" && buyer == "" && !strings.EqualFold(partner, "SOT") {
		return partner, "", nil
	}
	return "", "", errors.New("Choose exactly one: partner code or member. Internal orders require /order member:@buyer.")
}
