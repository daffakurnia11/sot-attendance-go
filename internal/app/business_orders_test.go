package app

import (
	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"math"
	"strings"
	"testing"
	"time"
)

func TestOrderPricingAndConversion(t *testing.T) {
	entries := []business.Entry{{ID: 1, Direction: "purchase", PartnerCode: "BZ", Price: 3100, Name: "Ammo Rifle", StockItemName: "Ammo Rifle", Unit: "clip"}, {ID: 2, Direction: "purchase", PartnerCode: "BZ", Price: 500, Name: "Joint", Unit: "piece"}}
	d, err := buildOrderDraft(entries, "purchase", "bz", "1:100,2:2", "2026-10-14", "2026-10-15", "discount", -1000)
	if err != nil {
		t.Fatal(err)
	}
	if d.Total != 310000 || d.Lines[0].Conversion != 180 || d.Lines[1].Conversion != 1 {
		t.Fatalf("wrong pricing/conversion: %+v", d)
	}
	entries[0].Price = 1
	if d.Lines[0].Entry.Price != 3100 {
		t.Fatal("order price changed with catalog")
	}
}
func TestOrderRejectsInvalidInput(t *testing.T) {
	entries := []business.Entry{{ID: 1, Direction: "sale", PartnerCode: "DW", Price: 15000, Unit: "piece"}}
	for _, tc := range []struct {
		name, items, partner, date string
		additional                 int64
		notes                      string
	}{
		{name: "duplicate", items: "1:1,1:1", partner: "DW", date: "2026-10-14"},
		{name: "negative quantity", items: "1:-1", partner: "DW", date: "2026-10-14"},
		{name: "wrong partner", items: "1:1", partner: "BZ", date: "2026-10-14"},
		{name: "bad date", items: "1:1", partner: "DW", date: "14/10/26"},
		{name: "missing cost reason", items: "1:1", partner: "DW", date: "2026-10-14", additional: 100},
		{name: "negative total", items: "1:1", partner: "DW", date: "2026-10-14", additional: -15001, notes: "discount"},
		{name: "overflow", items: "1:1", partner: "DW", date: "2026-10-14", additional: math.MaxInt64, notes: "fee"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildOrderDraft(entries, "sale", tc.partner, tc.items, tc.date, "2026-10-15", tc.notes, tc.additional); err == nil {
				t.Fatal("accepted invalid order")
			}
		})
	}
}
func TestOrderChannels(t *testing.T) {
	b := &Bot{businessSaleChannelID: "shared"}
	if !b.inBusinessOrderChannel("shared") || b.inBusinessOrderChannel("other") || (&Bot{}).inBusinessOrderChannel("") {
		t.Fatal("incorrect shared channel guard")
	}
	if orderCommandDirection("purchase") != "purchase" || orderCommandDirection("order") != "sale" || orderCommandDirection("invalid") != "" {
		t.Fatal("incorrect command direction")
	}
}

func TestOrderFormMenusAndSubmit(t *testing.T) {
	d := &orderDraft{Direction: "sale", Catalog: []business.Entry{
		{ID: 1, PartnerCode: "BZ", PartnerName: "Bladezay", Direction: "sale", StockGroup: "weapon", Name: "Vector", Unit: "piece", Price: 8000},
		{ID: 2, PartnerCode: "DW", PartnerName: "Dwarriors", Direction: "sale", StockGroup: "weapon", Name: "CRX", Unit: "piece", Price: 15000},
		{ID: 3, PartnerCode: "BZ", PartnerName: "Bladezay", Direction: "purchase", StockGroup: "ammo", Name: "Ammo", Unit: "clip"},
	}}
	rows := orderFormComponents("draft", d)
	if len(rows) != 1 {
		t.Fatalf("initial form rows=%d", len(rows))
	}
	buttons := rows[0].(discordgo.ActionsRow).Components
	if !buttons[1].(discordgo.Button).Disabled {
		t.Fatal("empty draft submit enabled")
	}
	d.Partner = "BZ"
	rows = orderFormComponents("draft", d)
	menu := rows[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if len(menu.Options) != 1 || menu.Options[0].Value != "1" {
		t.Fatalf("menu leaks another partner/direction: %+v", menu.Options)
	}
	d.Lines = []orderLine{{Entry: d.Catalog[0], Quantity: 1}}
	rows = orderFormComponents("draft", d)
	if rows[1].(discordgo.ActionsRow).Components[1].(discordgo.Button).Disabled {
		t.Fatal("complete draft submit disabled")
	}
	commands := orderSlashCommand(nil)
	if len(commands.Options) != 2 || commands.Options[0].Name != "partner" || commands.Options[0].Required || commands.Options[1].Type != discordgo.ApplicationCommandOptionUser {
		t.Fatal("order creation requires arguments")
	}

}

func TestOrderStatusesMatchDirection(t *testing.T) {
	for _, tc := range []struct{ direction, middle string }{{"sale", "ready"}, {"purchase", "paid"}} {
		statuses := orderStatuses(tc.direction)
		if len(statuses) != 4 || statuses[0] != "open" || statuses[1] != tc.middle || statuses[2] != "complete" {
			t.Fatalf("wrong statuses: %v", statuses)
		}
		d := &orderDraft{OrderID: 42, Direction: tc.direction, Status: tc.middle}
		buttons := orderStatusComponents(d)[0].(discordgo.ActionsRow).Components
		if len(buttons) != 2 || buttons[0].(discordgo.Button).CustomID != "orderstatus:42:cancelled" || buttons[1].(discordgo.Button).CustomID != "orderstatus:42:complete" {
			t.Fatalf("wrong persistent buttons: %+v", buttons)
		}
	}
	if len(orderStatuses("unknown")) != 0 {
		t.Fatal("unknown direction accepted")
	}
}

func TestOrderStatusConfirmationDoesNotDuplicateControls(t *testing.T) {
	response := orderStatusConfirmation(&orderDraft{OrderID: 2, Status: "complete"}, "office")
	if response.Content == nil || !strings.Contains(*response.Content, "**Complete**") {
		t.Fatal("missing saved status confirmation")
	}
	if response.Embeds == nil || len(*response.Embeds) != 0 || response.Components == nil || len(*response.Components) != 0 {
		t.Fatal("success retained duplicate order controls")
	}
}

func TestPurchaseProductsShownWithoutCategory(t *testing.T) {
	d := &orderDraft{Direction: "purchase", Partner: "CR", Catalog: []business.Entry{
		{ID: 1, PartnerCode: "CR", PartnerName: "Chaos", Direction: "purchase", StockGroup: "blueprint", Name: "Blueprint Vector", Unit: "piece"},
		{ID: 2, PartnerCode: "BZ", PartnerName: "Bladezay", Direction: "purchase", StockGroup: "ammo", Name: "Ammo Rifle", Unit: "clip"},
	}}
	rows := orderFormComponents("draft", d)
	if len(rows) != 2 {
		t.Fatalf("want product/buttons, got %d rows", len(rows))
	}
	menu := rows[0].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if menu.CustomID != "orderform:product:draft" || len(menu.Options) != 1 || menu.Options[0].Value != "1" {
		t.Fatalf("unexpected products: %+v", menu)
	}
}

func TestOrderEmbedHasNoDirectionField(t *testing.T) {
	for _, direction := range []string{"sale", "purchase"} {
		d := &orderDraft{Direction: direction}
		embed := orderEmbed(d)
		for _, field := range embed.Fields {
			if field.Name == "Direction" {
				t.Fatal("direction field remains")
			}
		}
		if direction == "sale" && embed.Color != 0x57F287 || direction == "purchase" && embed.Color != 0xED4245 {
			t.Fatal("incorrect embed color")
		}
	}
}

func TestRequiredPartnerCodeValidation(t *testing.T) {
	entries := []business.Entry{{PartnerCode: "CR", Direction: "purchase"}, {PartnerCode: "GWF", Direction: "sale"}}
	code, err := orderPartnerCode(entries, "purchase", " cr ")
	if err != nil || code != "CR" {
		t.Fatalf("normalized code: %q %v", code, err)
	}
	for _, raw := range []string{"", "unknown", "GWF"} {
		if _, err := orderPartnerCode(entries, "purchase", raw); err == nil {
			t.Fatalf("accepted unavailable partner %q", raw)
		}
	}
}

func TestOrderPickupDefaultsAcrossMonth(t *testing.T) {
	date, err := time.Parse("2006-01-02", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if got := defaultOrderPickup("sale", date).Format("2006-01-02"); got != "2027-01-02" {
		t.Fatal(got)
	}
	if !defaultOrderPickup("purchase", date).Equal(date) {
		t.Fatal("purchase default changed")
	}
	if orderMoney(1700000) != "$1,700,000" || orderMoney(-10000) != "$-10,000" {
		t.Fatal("incorrect currency format")
	}
}

func TestOrderAdditionalPercentages(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		subtotal, want int64
	}{{"+15%", 225000, 33750}, {"-10%", 225000, -22500}, {"+15000", 225000, 15000}, {"2.5%", 100, 3}, {"-2.5%", 100, -3}} {
		got, err := parseOrderAdditional(tc.raw, tc.subtotal)
		if err != nil || got != tc.want {
			t.Fatalf("%s: %d %v", tc.raw, got, err)
		}
	}
	for _, raw := range []string{"abc", "15%%", "1.234%", "1e3%"} {
		if _, err := parseOrderAdditional(raw, 100); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	d := &orderDraft{Direction: "sale", Partner: "BZ", Date: time.Now(), Pickup: time.Now(), Notes: "fast track", AdditionalInput: "+15%", Catalog: []business.Entry{{ID: 1, Direction: "sale", PartnerCode: "BZ", Price: 9000, Unit: "piece"}}}
	d.Lines = []orderLine{{Entry: d.Catalog[0], Quantity: 25}}
	if err := recalculateOrder(d); err != nil || d.Total != 258750 {
		t.Fatalf("initial percentage: %+v %v", d, err)
	}
	d.Lines[0].Quantity = 50
	if err := recalculateOrder(d); err != nil || d.Total != 517500 {
		t.Fatalf("updated percentage: %+v %v", d, err)
	}
}

func TestOrderFooterUsesPersistedTimesInWIB(t *testing.T) {
	created := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	d := &orderDraft{Direction: "sale", CreatedAt: created, UpdatedAt: created.Add(time.Hour), UpdatedBy: "DeltaKilo"}
	embed := orderEmbed(d)
	if embed.Footer == nil {
		t.Fatal("missing footer")
	}
	for _, want := range []string{"Created: 15 Oct 2026 10:00:00 WIB", "Updated: 15 Oct 2026 11:00:00 WIB", "Last updated by: DeltaKilo"} {
		if !strings.Contains(embed.Footer.Text, want) {
			t.Fatalf("missing %q: %s", want, embed.Footer.Text)
		}
	}
	d.UpdatedBy = ""
	if !strings.Contains(orderEmbed(d).Footer.Text, "Unknown") {
		t.Fatal("invented historical updater")
	}
}

func TestInternalOrderPackagesAndAlacarte(t *testing.T) {
	source := []business.Entry{
		{ID: 1, PartnerCode: "SOT", Direction: "internal", Name: "Vector package", Unit: "package", Price: 18500, StockItemID: 10, Components: []business.PackageComponent{{StockItemID: 11, Name: "Ammo SMG", Quantity: 300}, {StockItemID: 12, Name: "Vest", Quantity: 2}, {StockItemID: 13, Name: "Joint", Quantity: 2}}},
		{ID: 2, PartnerCode: "SOT", Direction: "internal", Name: "Ammo SMG", StockItemName: "Ammo SMG", Unit: "clip", Price: 2500, StockItemID: 11},
	}
	catalog := orderCatalog(source)
	if source[0].Direction != "internal" {
		t.Fatal("repository catalog mutated")
	}
	partner, err := orderPartnerCode(catalog, "sale", "sot")
	if err != nil || partner != "SOT" {
		t.Fatal("internal partner unavailable")
	}
	if _, err = orderPartnerCode(catalog, "purchase", "SOT"); err == nil {
		t.Fatal("internal purchase allowed")
	}
	draft, err := buildOrderDraft(catalog, "sale", "SOT", "1:2,2:1", "2026-10-22", "2026-10-24", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Total != 39500 || len(draft.Lines) != 2 || draft.Lines[1].Conversion != 150 {
		t.Fatalf("incorrect internal draft: %+v", draft)
	}
	if orderEmbed(draft).Color != 0x3498DB {
		t.Fatal("internal embed must be blue")
	}
	catalog[0].Components = nil
	if _, err = buildOrderDraft(catalog, "sale", "SOT", "1:1", "2026-10-22", "2026-10-24", "", 0); err == nil {
		t.Fatal("package missing stock components accepted")
	}
	catalog[0].Components = []business.PackageComponent{{StockItemID: 11, Quantity: 300}}
	if _, err = buildOrderDraft(catalog, "sale", "SOT", "1:10000000", "2026-10-22", "2026-10-24", "", 0); err == nil {
		t.Fatal("package stock overflow accepted")
	}
}

func TestOrderMemberTarget(t *testing.T) {
	member := &discordgo.ApplicationCommandInteractionDataOption{Name: "member", Type: discordgo.ApplicationCommandOptionUser, Value: "123"}
	partner := &discordgo.ApplicationCommandInteractionDataOption{Name: "partner", Type: discordgo.ApplicationCommandOptionString, Value: "GWF"}
	code, buyer, err := orderTarget([]*discordgo.ApplicationCommandInteractionDataOption{member}, "sale")
	if err != nil || code != "SOT" || buyer != "123" {
		t.Fatal("member target not resolved")
	}
	for _, opts := range [][]*discordgo.ApplicationCommandInteractionDataOption{nil, {member, partner}, {{Name: "partner", Type: discordgo.ApplicationCommandOptionString, Value: "SOT"}}} {
		if _, _, err := orderTarget(opts, "sale"); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
	if _, _, err := orderTarget([]*discordgo.ApplicationCommandInteractionDataOption{member}, "purchase"); err == nil {
		t.Fatal("purchase member accepted")
	}
	embed := orderEmbed(&orderDraft{Direction: "sale", Partner: "SOT", BuyerDiscordID: "123"})
	if embed.Fields[0].Name != "Buyer" || embed.Fields[0].Value != "<@123>" || embed.Color != 0x3498DB {
		t.Fatal("buyer embed incorrect")
	}
}

func TestOrderCancellationButtons(t *testing.T) {
	for _, dir := range []string{"sale", "purchase"} {
		for _, status := range []string{"open"} {
			buttons := orderStatusComponents(&orderDraft{OrderID: 1, Direction: dir, Status: status})[0].(discordgo.ActionsRow).Components
			if buttons[0].(discordgo.Button).Label != "Cancel" {
				t.Fatal("missing cancel")
			}
			if status == "complete" && len(buttons) != 1 {
				t.Fatal("completed order can advance")
			}
		}
	}
	if len(orderStatusComponents(&orderDraft{Status: "complete"})) != 0 {
		t.Fatal("completed order has controls")
	}
	if len(orderStatusComponents(&orderDraft{Status: "cancelled"})) != 0 {
		t.Fatal("cancelled order has controls")
	}
}
