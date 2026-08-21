package crafting

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ database *pgxpool.Pool }

func NewRepository(database *pgxpool.Pool) *Repository { return &Repository{database: database} }

func (r *Repository) List(ctx context.Context) ([]RecipeSummary, error) {
	const query = `
		SELECT weapon_code, weapon_name, output_quantity,
			EXTRACT(EPOCH FROM crafting_time)::bigint
		FROM crafting_recipes
		ORDER BY weapon_name, weapon_code`
	rows, err := r.database.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query crafting recipes: %w", err)
	}
	defer rows.Close()
	recipes := make([]RecipeSummary, 0)
	for rows.Next() {
		var recipe RecipeSummary
		if err := rows.Scan(&recipe.WeaponCode, &recipe.WeaponName, &recipe.OutputQuantity, &recipe.CraftingTimeSeconds); err != nil {
			return nil, fmt.Errorf("scan crafting recipe: %w", err)
		}
		recipes = append(recipes, recipe)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate crafting recipes: %w", err)
	}
	return recipes, nil
}

func (r *Repository) Get(ctx context.Context, weaponCode string) (Recipe, error) {
	const recipeQuery = `
		SELECT weapon_code, weapon_name, output_quantity,
			EXTRACT(EPOCH FROM crafting_time)::bigint
		FROM crafting_recipes
		WHERE weapon_code = $1`
	var recipe Recipe
	err := r.database.QueryRow(ctx, recipeQuery, weaponCode).Scan(
		&recipe.WeaponCode, &recipe.WeaponName, &recipe.OutputQuantity, &recipe.CraftingTimeSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Recipe{}, ErrNotFound
	}
	if err != nil {
		return Recipe{}, fmt.Errorf("query crafting recipe: %w", err)
	}
	const ingredientQuery = `
		SELECT item_code, item_name, quantity
		FROM crafting_recipe_items
		WHERE crafting_recipe_id = (
			SELECT id FROM crafting_recipes WHERE weapon_code = $1
		)
		ORDER BY item_name, item_code`
	rows, err := r.database.Query(ctx, ingredientQuery, weaponCode)
	if err != nil {
		return Recipe{}, fmt.Errorf("query crafting ingredients: %w", err)
	}
	defer rows.Close()
	recipe.Ingredients = make([]Ingredient, 0)
	for rows.Next() {
		var ingredient Ingredient
		if err := rows.Scan(&ingredient.ItemCode, &ingredient.ItemName, &ingredient.Quantity); err != nil {
			return Recipe{}, fmt.Errorf("scan crafting ingredient: %w", err)
		}
		recipe.Ingredients = append(recipe.Ingredients, ingredient)
	}
	if err := rows.Err(); err != nil {
		return Recipe{}, fmt.Errorf("iterate crafting ingredients: %w", err)
	}
	return recipe, nil
}
