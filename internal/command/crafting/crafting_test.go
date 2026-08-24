package crafting

import (
	"strings"
	"testing"

	craftingdomain "github.com/daffakurniawan/sot-discord-bot/internal/crafting"
)

func TestParseMultipleProducts(t *testing.T) {
	t.Parallel()

	items, err := Parse(" !craft Vector:30 mp9:20 crx-mk2:5 ", "!")
	if err != nil {
		t.Fatal(err)
	}
	want := []craftingdomain.BatchItem{{WeaponCode: "vector", Quantity: 30}, {WeaponCode: "mp9", Quantity: 20}, {WeaponCode: "crx_mk2", Quantity: 5}}
	if len(items) != len(want) {
		t.Fatalf("Parse() = %#v", items)
	}
	for index := range want {
		if items[index] != want[index] {
			t.Fatalf("Parse()[%d] = %#v, want %#v", index, items[index], want[index])
		}
	}
}

func TestParseRejectsInvalidSyntax(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"!craft", "!craft vector", "!craft vector:0", "!craft vector:nope"} {
		if _, err := Parse(content, "!"); err == nil {
			t.Errorf("Parse(%q) expected error", content)
		}
	}
}

func TestEmbedShowsCombinedTotalsWithinDiscordFieldLimits(t *testing.T) {
	t.Parallel()

	embed := Embed(craftingdomain.BatchCalculation{
		Recipes:                  []craftingdomain.Calculation{{WeaponName: "Vector", RequestedQuantity: 30}, {WeaponName: "MP9", RequestedQuantity: 20}},
		Ingredients:              []craftingdomain.TotalIngredient{{ItemName: "Iron", TotalQuantity: 2000}},
		TotalRequestedQuantity:   50,
		TotalCraftCount:          50,
		TotalCraftingTimeSeconds: 400,
	})
	if embed.Title != "Crafting Plan" || len(embed.Fields) != 3 {
		t.Fatalf("Embed() = %#v", embed)
	}
	if !strings.Contains(embed.Fields[0].Value, "Vector") || !strings.Contains(embed.Fields[1].Value, "2,000") || !strings.Contains(embed.Fields[2].Value, "6m 40s") {
		t.Fatalf("Embed() fields = %#v", embed.Fields)
	}
	for _, field := range embed.Fields {
		if len(field.Value) > 1024 {
			t.Errorf("field %q length = %d", field.Name, len(field.Value))
		}
	}
}
