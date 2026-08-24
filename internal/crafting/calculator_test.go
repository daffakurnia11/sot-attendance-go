package crafting

import (
	"context"
	"errors"
	"testing"
)

type batchStore struct{ recipes map[string]Recipe }

func (s batchStore) List(context.Context) ([]RecipeSummary, error) { return nil, nil }
func (s batchStore) Get(_ context.Context, code string) (Recipe, error) {
	recipe, exists := s.recipes[code]
	if !exists {
		return Recipe{}, ErrNotFound
	}
	return recipe, nil
}

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

func TestCalculateBatchLoadsAndCombinesRecipes(t *testing.T) {
	t.Parallel()
	store := batchStore{recipes: map[string]Recipe{
		"vector": {RecipeSummary: RecipeSummary{WeaponCode: "vector", WeaponName: "Vector", OutputQuantity: 1, CraftingTimeSeconds: 8}, Ingredients: []Ingredient{{ItemCode: "iron", ItemName: "Iron", Quantity: 40}}},
		"mp9":    {RecipeSummary: RecipeSummary{WeaponCode: "mp9", WeaponName: "MP9", OutputQuantity: 1, CraftingTimeSeconds: 8}, Ingredients: []Ingredient{{ItemCode: "iron", ItemName: "Iron", Quantity: 40}}},
	}}
	result, err := CalculateBatch(context.Background(), store, []BatchItem{{WeaponCode: " VECTOR ", Quantity: 2}, {WeaponCode: "mp9", Quantity: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalRequestedQuantity != 5 || len(result.Ingredients) != 1 || result.Ingredients[0].TotalQuantity != 200 {
		t.Fatalf("CalculateBatch() = %#v", result)
	}
}

func TestCalculateBatchRejectsDuplicateAndUnknownRecipes(t *testing.T) {
	t.Parallel()
	store := batchStore{recipes: map[string]Recipe{"vector": {RecipeSummary: RecipeSummary{WeaponCode: "vector", OutputQuantity: 1, CraftingTimeSeconds: 8}}}}
	if _, err := CalculateBatch(context.Background(), store, []BatchItem{{WeaponCode: "vector", Quantity: 1}, {WeaponCode: "vector", Quantity: 2}}); !errors.Is(err, ErrDuplicateRecipe) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := CalculateBatch(context.Background(), store, []BatchItem{{WeaponCode: "missing", Quantity: 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found error = %v", err)
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
