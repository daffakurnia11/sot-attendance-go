package crafting

import "testing"

func TestCalculateScalesRecipeAndRoundsCraftCount(t *testing.T) {
	recipe := Recipe{RecipeSummary: RecipeSummary{WeaponCode: "test", WeaponName: "Test", OutputQuantity: 2, CraftingTimeSeconds: 8}, Ingredients: []Ingredient{{ItemCode: "iron", ItemName: "Iron", Quantity: 25}}}
	result, err := Calculate(recipe, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result.CraftCount != 3 || result.CraftingTimeSeconds != 24 || result.Ingredients[0].TotalQuantity != 75 {
		t.Fatalf("Calculate() = %#v", result)
	}
}

func TestCalculateRejectsUnsafeInput(t *testing.T) {
	valid := Recipe{RecipeSummary: RecipeSummary{OutputQuantity: 1, CraftingTimeSeconds: 8}}
	for _, quantity := range []int64{0, MaxQuantity + 1} {
		if _, err := Calculate(valid, quantity); err == nil {
			t.Fatalf("Calculate(_, %d) expected error", quantity)
		}
	}
	invalid := Recipe{RecipeSummary: RecipeSummary{OutputQuantity: 0, CraftingTimeSeconds: 8}}
	if _, err := Calculate(invalid, 1); err == nil {
		t.Fatal("Calculate() expected invalid recipe error")
	}
}

func TestCombineTotalsSharedIngredients(t *testing.T) {
	result, err := Combine([]Calculation{
		{WeaponCode: "one", RequestedQuantity: 2, CraftCount: 2, CraftingTimeSeconds: 16, Ingredients: []CalculatedIngredient{{ItemCode: "iron", ItemName: "Iron", TotalQuantity: 50}, {ItemCode: "blueprint", ItemName: "Blueprint Pistol", TotalQuantity: 2}}},
		{WeaponCode: "two", RequestedQuantity: 3, CraftCount: 3, CraftingTimeSeconds: 24, Ingredients: []CalculatedIngredient{{ItemCode: "iron", ItemName: "Iron", TotalQuantity: 120}, {ItemCode: "rubber", ItemName: "Rubber", TotalQuantity: 60}, {ItemCode: "blueprint", ItemName: "Blueprint Rifle", TotalQuantity: 3}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRequestedQuantity != 5 || result.TotalCraftCount != 5 || result.TotalCraftingTimeSeconds != 40 {
		t.Fatalf("Combine() totals = %#v", result)
	}
	if len(result.Ingredients) != 4 || result.Ingredients[0].TotalQuantity != 170 || result.Ingredients[1].TotalQuantity != 2 || result.Ingredients[2].TotalQuantity != 60 || result.Ingredients[3].TotalQuantity != 3 {
		t.Fatalf("Combine() ingredients = %#v", result.Ingredients)
	}
}
