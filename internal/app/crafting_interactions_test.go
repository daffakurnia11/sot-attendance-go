package app

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	craftingdomain "github.com/daffakurniawan/sot-discord-bot/internal/crafting"
)

func TestCraftDraftStoreUpsertRemoveAndExpire(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	store := newCraftDraftStore(10 * time.Minute)
	store.nowFunc = func() time.Time { return now }
	if err := store.upsert("guild:user", "vector", 30); err != nil {
		t.Fatal(err)
	}
	if err := store.upsert("guild:user", "vector", 40); err != nil {
		t.Fatal(err)
	}
	if err := store.upsert("guild:user", "mp9", 20); err != nil {
		t.Fatal(err)
	}
	items := store.get("guild:user")
	if len(items) != 2 || items[0].Quantity != 40 {
		t.Fatalf("draft = %#v", items)
	}
	store.remove("guild:user", "vector")
	if items = store.get("guild:user"); len(items) != 1 || items[0].WeaponCode != "mp9" {
		t.Fatalf("removed draft = %#v", items)
	}
	now = now.Add(11 * time.Minute)
	if items = store.get("guild:user"); len(items) != 0 {
		t.Fatalf("expired draft = %#v", items)
	}
}

func TestCraftBuilderMessageUsesRecipeOptionsAndDraftControls(t *testing.T) {
	t.Parallel()

	embed, components := craftBuilderMessage(
		[]craftingdomain.RecipeSummary{{WeaponCode: "vector", WeaponName: "Vector", CraftingTimeSeconds: 8}},
		[]craftingdomain.BatchItem{{WeaponCode: "vector", Quantity: 30}},
	)
	if embed.Title != "Crafting Calculator" || len(components) != 3 {
		t.Fatalf("builder = %#v, components = %#v", embed, components)
	}
}

func TestModalTextValue(t *testing.T) {
	t.Parallel()

	value, err := modalTextValue([]discordgo.MessageComponent{
		&discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			&discordgo.TextInput{CustomID: craftQuantityID, Value: "30"},
		}},
	}, craftQuantityID)
	if err != nil {
		t.Fatal(err)
	}
	if value != "30" {
		t.Fatalf("modal value = %q, want 30", value)
	}
}
