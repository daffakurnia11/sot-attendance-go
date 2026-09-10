package stash

import (
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

func TestChannelWarningNamesTheOneAllowedAction(t *testing.T) {
	t.Parallel()
	deposit := ChannelWarning("123", "public", stockdomain.ActionDeposit)
	want := "<@123> This channel only records **Public Stash deposits**. " +
		"Run `/stash deposit` and pick the items from the menu. " +
		"This notice disappears shortly."
	if deposit != want {
		t.Fatalf("ChannelWarning() = %q\nwant %q", deposit, want)
	}
	// No prefix command is offered: there is no way to write an item name.
	withdraw := ChannelWarning("456", "boss", stockdomain.ActionWithdraw)
	if !strings.Contains(withdraw, "**Boss Stash withdraws**") || !strings.Contains(withdraw, "`/stash withdraw`") {
		t.Fatalf("ChannelWarning() = %q", withdraw)
	}
	if strings.Contains(withdraw, "!stash") || strings.Contains(withdraw, ":1000") {
		t.Fatalf("ChannelWarning() still offers a written command: %q", withdraw)
	}
}
