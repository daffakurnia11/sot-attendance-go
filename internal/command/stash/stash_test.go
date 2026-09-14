package stash

import (
	"reflect"
	"strings"
	"testing"

	stockdomain "github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

func testCatalogItems() []Item {
	return []Item{
		{Safebox: "public", ItemKey: "copper", Name: "Copper", Group: "crafting", Quantity: 7346},
		{Safebox: "public", ItemKey: "metal_scrap", Name: "Metal Scrap", Group: "crafting", Quantity: 5572},
		{Safebox: "public", ItemKey: "ammo_smg", Name: "Ammo SMG", Group: "ammo", Quantity: 6150},
		{Safebox: "public", ItemKey: "pistol", Name: "Pistol Karet", Group: "weapon", Quantity: 5},
	}
}

func TestCatalogHoldsOnlyStoredItems(t *testing.T) {
	t.Parallel()
	catalog := NewCatalog(testCatalogItems())
	for _, itemKey := range []string{"copper", "metal_scrap", "ammo_smg", "pistol"} {
		if !catalog.Holds(itemKey) {
			t.Errorf("Holds(%q) = false", itemKey)
		}
	}
	// Nothing normalizes: every key reaching a catalog came from a menu the
	// bot filled, so a near miss is a row that is gone, not a spelling.
	for _, itemKey := range []string{"", "Copper", "metal scrap", "gold"} {
		if catalog.Holds(itemKey) {
			t.Errorf("Holds(%q) = true", itemKey)
		}
	}
}

func TestCatalogNameFallsBackToKey(t *testing.T) {
	t.Parallel()
	catalog := NewCatalog(testCatalogItems())
	if got := catalog.Name("copper"); got != "Copper" {
		t.Errorf("Name(copper) = %q", got)
	}
	if got := catalog.Name("gold"); got != "gold" {
		t.Errorf("Name(gold) = %q", got)
	}
	if catalog.Len() != 4 {
		t.Errorf("Len() = %d, want 4", catalog.Len())
	}
}

func TestGroupsAndInGroupPreserveRepositoryOrder(t *testing.T) {
	t.Parallel()
	items := testCatalogItems()
	groups := Groups(items)
	if len(groups) != 3 || groups[0] != "crafting" || groups[1] != "ammo" || groups[2] != "weapon" {
		t.Fatalf("Groups() = %#v", groups)
	}
	crafting := InGroup(items, "crafting")
	if len(crafting) != 2 || crafting[0].ItemKey != "copper" || crafting[1].ItemKey != "metal_scrap" {
		t.Fatalf("InGroup(crafting) = %#v", crafting)
	}
	if len(InGroup(items, "blueprint")) != 0 {
		t.Fatal("InGroup(blueprint) is not empty")
	}
}

func TestTransactionEmbedShowsResultingQuantity(t *testing.T) {
	t.Parallel()
	embed := TransactionEmbed("boss", stockdomain.ActionWithdraw, []Line{{Name: "Ammo SMG", Quantity: 1000, After: 4611}}, "Kenji Nakamura", "123", "boss order")
	if embed.Title != "Withdraw · Boss Stash" || embed.Color != 0xED4245 {
		t.Fatalf("unexpected title or color: %#v", embed)
	}
	if embed.Description != "Purpose: boss order\nBy **Kenji Nakamura** · <@123>" {
		t.Fatalf("unexpected description: %q", embed.Description)
	}
	if len(embed.Fields) != 1 || embed.Fields[0].Value != "**Ammo SMG** × 1,000 → 4,611" {
		t.Fatalf("unexpected fields: %#v", embed.Fields)
	}
}

func TestBalanceEmbedGroupsItems(t *testing.T) {
	t.Parallel()
	embed := BalanceEmbed("public", []stockdomain.Item{
		{Safebox: "public", ItemKey: "copper", Name: "Copper", Group: "crafting", Quantity: 7346},
		{Safebox: "public", ItemKey: "iron", Name: "Iron", Group: "crafting", Quantity: 6074},
		{Safebox: "public", ItemKey: "ammo_smg", Name: "Ammo SMG", Group: "ammo", Quantity: 6150},
	})
	if embed.Title != "Public Stash Balance" || len(embed.Fields) != 2 {
		t.Fatalf("unexpected embed: %#v", embed)
	}
	if embed.Fields[0].Name != "Crafting" || embed.Fields[0].Value != "**Copper** — 7,346\n**Iron** — 6,074" {
		t.Fatalf("unexpected crafting field: %#v", embed.Fields[0])
	}
	if embed.Fields[1].Name != "Ammo" || embed.Fields[1].Value != "**Ammo SMG** — 6,150" {
		t.Fatalf("unexpected ammo field: %#v", embed.Fields[1])
	}
}

func TestBalanceEmbedWithoutItems(t *testing.T) {
	t.Parallel()
	embed := BalanceEmbed("boss", nil)
	if len(embed.Fields) != 1 || embed.Fields[0].Value != "None" {
		t.Fatalf("unexpected embed: %#v", embed)
	}
}

func TestChannelWarningNamesTheSafeboxAndBothCommands(t *testing.T) {
	t.Parallel()
	public := ChannelWarning("123", "public")
	want := "<@123> This channel only records **Public Stash** movements. " +
		"Run `/stash deposit` or `/stash withdraw` and pick the items from the menu. " +
		"This notice disappears shortly."
	if public != want {
		t.Fatalf("ChannelWarning() = %q\nwant %q", public, want)
	}
	// The channel no longer fixes the action, so both are offered.
	boss := ChannelWarning("456", "boss")
	if !strings.Contains(boss, "**Boss Stash**") || !strings.Contains(boss, "`/stash deposit`") || !strings.Contains(boss, "`/stash withdraw`") {
		t.Fatalf("ChannelWarning() = %q", boss)
	}
	// No prefix command is offered: there is no way to write an item name.
	if strings.Contains(boss, "!stash") || strings.Contains(boss, ":1000") {
		t.Fatalf("ChannelWarning() still offers a written command: %q", boss)
	}
}

// Discord packs inline fields three to a row, so a second column is made by
// padding each row out with an empty field.
func TestBalanceEmbedLaysOutTwoColumns(t *testing.T) {
	t.Parallel()
	embed := BalanceEmbed("public", []stockdomain.Item{
		{Group: "crafting", Name: "Copper", Quantity: 1},
		{Group: "ammo", Name: "Ammo SMG", Quantity: 1},
		{Group: "weapon", Name: "MP9", Quantity: 1},
		{Group: "blueprint", Name: "Blueprint MP9", Quantity: 1},
		{Group: "thief_tools", Name: "USB", Quantity: 1},
	})
	names := make([]string, 0, len(embed.Fields))
	for _, field := range embed.Fields {
		if !field.Inline {
			t.Fatalf("field %q is not inline, so it cannot share a row", field.Name)
		}
		names = append(names, field.Name)
	}
	// A spacer after every second group, and none before the first or trailing
	// the last: five groups fill two rows and open a third.
	if want := []string{"Crafting", "Ammo", "​", "Weapons", "Blueprints", "​", "Thief Tools"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("field names = %#v, want %#v", names, want)
	}
}

// A group where everything reads zero says nothing a reader can act on, and
// there are enough of those to push the stocked groups off the screen.
func TestBalanceEmbedHidesGroupsWithNoStock(t *testing.T) {
	t.Parallel()
	embed := BalanceEmbed("boss", []stockdomain.Item{
		{Group: "crafting", Name: "Copper", Quantity: 0},
		{Group: "crafting", Name: "Iron", Quantity: 0},
		{Group: "ammo", Name: "Ammo SMG", Quantity: 0},
		{Group: "ammo", Name: "Ammo Rifle", Quantity: 5},
		{Group: "weapon", Name: "MP9", Quantity: 0},
	})
	if len(embed.Fields) != 1 || embed.Fields[0].Name != "Ammo" {
		t.Fatalf("fields = %#v, want the ammo group alone", embed.Fields)
	}
	// The zero item stays listed inside a group that has any stock at all.
	if embed.Fields[0].Value != "**Ammo SMG** — 0\n**Ammo Rifle** — 5" {
		t.Fatalf("ammo field = %q", embed.Fields[0].Value)
	}
}

// Every group empty is the same as having no items: the embed still says so
// rather than rendering nothing.
func TestBalanceEmbedWithEverythingAtZero(t *testing.T) {
	t.Parallel()
	embed := BalanceEmbed("boss", []stockdomain.Item{
		{Group: "crafting", Name: "Copper", Quantity: 0},
		{Group: "ammo", Name: "Ammo SMG", Quantity: 0},
	})
	if len(embed.Fields) != 1 || embed.Fields[0].Value != "None" {
		t.Fatalf("fields = %#v, want the empty placeholder", embed.Fields)
	}
}

// The new groups are named, not spelled out of their key.
func TestGroupLabelNamesEveryGroup(t *testing.T) {
	t.Parallel()
	for group, want := range map[string]string{
		"thief_tools":        "Thief Tools",
		"weapon_accessories": "Weapon Accessories",
		"body_drugs":         "Body & Drugs",
	} {
		if got := GroupLabel(group); got != want {
			t.Errorf("GroupLabel(%q) = %q, want %q", group, got, want)
		}
	}
}
