package crafting

import (
	"errors"
	"fmt"
	"math"
)

const MaxQuantity = 10_000
const MaxRecipes = 20

var ErrNotFound = errors.New("crafting recipe not found")

type RecipeSummary struct {
	WeaponCode          string `json:"weapon_code"`
	WeaponName          string `json:"weapon_name"`
	OutputQuantity      int64  `json:"output_quantity"`
	CraftingTimeSeconds int64  `json:"crafting_time_seconds"`
}

type Ingredient struct {
	ItemCode string
	ItemName string
	Quantity int64
}

type Recipe struct {
	RecipeSummary
	Ingredients []Ingredient
}

type CalculatedIngredient struct {
	ItemCode         string `json:"item_code"`
	ItemName         string `json:"item_name"`
	QuantityPerCraft int64  `json:"quantity_per_craft"`
	TotalQuantity    int64  `json:"total_quantity"`
}

type Calculation struct {
	WeaponCode             string                 `json:"weapon_code"`
	WeaponName             string                 `json:"weapon_name"`
	RequestedQuantity      int64                  `json:"requested_quantity"`
	OutputQuantityPerCraft int64                  `json:"output_quantity_per_craft"`
	CraftCount             int64                  `json:"craft_count"`
	CraftingTimeSeconds    int64                  `json:"crafting_time_seconds"`
	Ingredients            []CalculatedIngredient `json:"ingredients"`
}

type BatchItem struct {
	WeaponCode string `json:"weapon_code"`
	Quantity   int64  `json:"quantity"`
}

type BatchCalculation struct {
	Recipes                  []Calculation     `json:"recipes"`
	TotalRequestedQuantity   int64             `json:"total_requested_quantity"`
	TotalCraftCount          int64             `json:"total_craft_count"`
	TotalCraftingTimeSeconds int64             `json:"total_crafting_time_seconds"`
	Ingredients              []TotalIngredient `json:"ingredients"`
}

type TotalIngredient struct {
	ItemCode      string `json:"item_code"`
	ItemName      string `json:"item_name"`
	TotalQuantity int64  `json:"total_quantity"`
}

func Combine(calculations []Calculation) (BatchCalculation, error) {
	if len(calculations) < 1 || len(calculations) > MaxRecipes {
		return BatchCalculation{}, fmt.Errorf("recipe count must be between 1 and %d", MaxRecipes)
	}
	result := BatchCalculation{Recipes: calculations, Ingredients: make([]TotalIngredient, 0)}
	ingredientIndexes := make(map[string]int)
	for _, calculation := range calculations {
		if calculation.RequestedQuantity > math.MaxInt64-result.TotalRequestedQuantity || calculation.CraftCount > math.MaxInt64-result.TotalCraftCount || calculation.CraftingTimeSeconds > math.MaxInt64-result.TotalCraftingTimeSeconds {
			return BatchCalculation{}, errors.New("combined crafting totals exceed supported range")
		}
		result.TotalRequestedQuantity += calculation.RequestedQuantity
		result.TotalCraftCount += calculation.CraftCount
		result.TotalCraftingTimeSeconds += calculation.CraftingTimeSeconds
		for _, ingredient := range calculation.Ingredients {
			key := ingredient.ItemCode + "\x00" + ingredient.ItemName
			index, exists := ingredientIndexes[key]
			if !exists {
				ingredientIndexes[key] = len(result.Ingredients)
				result.Ingredients = append(result.Ingredients, TotalIngredient{ItemCode: ingredient.ItemCode, ItemName: ingredient.ItemName, TotalQuantity: ingredient.TotalQuantity})
				continue
			}
			if ingredient.TotalQuantity > math.MaxInt64-result.Ingredients[index].TotalQuantity {
				return BatchCalculation{}, fmt.Errorf("combined ingredient %q exceeds supported range", ingredient.ItemCode)
			}
			result.Ingredients[index].TotalQuantity += ingredient.TotalQuantity
		}
	}
	return result, nil
}

func Calculate(recipe Recipe, requestedQuantity int64) (Calculation, error) {
	if requestedQuantity < 1 || requestedQuantity > MaxQuantity {
		return Calculation{}, fmt.Errorf("quantity must be between 1 and %d", MaxQuantity)
	}
	if recipe.OutputQuantity < 1 || recipe.CraftingTimeSeconds < 1 {
		return Calculation{}, errors.New("recipe has invalid output quantity or crafting time")
	}
	craftCount := (requestedQuantity + recipe.OutputQuantity - 1) / recipe.OutputQuantity
	if craftCount > math.MaxInt64/recipe.CraftingTimeSeconds {
		return Calculation{}, errors.New("calculated crafting time exceeds supported range")
	}
	result := Calculation{
		WeaponCode: recipe.WeaponCode, WeaponName: recipe.WeaponName,
		RequestedQuantity: requestedQuantity, OutputQuantityPerCraft: recipe.OutputQuantity,
		CraftCount: craftCount, CraftingTimeSeconds: craftCount * recipe.CraftingTimeSeconds,
		Ingredients: make([]CalculatedIngredient, 0, len(recipe.Ingredients)),
	}
	for _, ingredient := range recipe.Ingredients {
		if ingredient.Quantity < 1 || craftCount > math.MaxInt64/ingredient.Quantity {
			return Calculation{}, fmt.Errorf("ingredient %q has invalid or unsupported quantity", ingredient.ItemCode)
		}
		result.Ingredients = append(result.Ingredients, CalculatedIngredient{
			ItemCode: ingredient.ItemCode, ItemName: ingredient.ItemName,
			QuantityPerCraft: ingredient.Quantity, TotalQuantity: ingredient.Quantity * craftCount,
		})
	}
	return result, nil
}
