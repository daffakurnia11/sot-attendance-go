CREATE TABLE IF NOT EXISTS crafting_recipes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    weapon_code TEXT NOT NULL,
    weapon_name TEXT NOT NULL,
    output_quantity INTEGER NOT NULL DEFAULT 1,
    crafting_time INTERVAL NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT crafting_recipes_weapon_code_not_blank CHECK (
        BTRIM(weapon_code) <> ''
    ),
    CONSTRAINT crafting_recipes_weapon_name_not_blank CHECK (
        BTRIM(weapon_name) <> ''
    ),
    CONSTRAINT crafting_recipes_output_quantity_positive CHECK (
        output_quantity > 0
    ),
    CONSTRAINT crafting_recipes_crafting_time_positive CHECK (
        crafting_time > INTERVAL '0 seconds'
    ),
    CONSTRAINT crafting_recipes_weapon_code_unique UNIQUE (weapon_code)
);

CREATE TABLE IF NOT EXISTS crafting_recipe_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    crafting_recipe_id BIGINT NOT NULL,
    item_code TEXT NOT NULL,
    item_name TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT crafting_recipe_items_recipe_foreign_key
        FOREIGN KEY (crafting_recipe_id)
        REFERENCES crafting_recipes (id) ON DELETE CASCADE,
    CONSTRAINT crafting_recipe_items_item_code_not_blank CHECK (
        BTRIM(item_code) <> ''
    ),
    CONSTRAINT crafting_recipe_items_item_name_not_blank CHECK (
        BTRIM(item_name) <> ''
    ),
    CONSTRAINT crafting_recipe_items_quantity_positive CHECK (quantity > 0),
    CONSTRAINT crafting_recipe_items_recipe_item_unique UNIQUE (
        crafting_recipe_id,
        item_code
    )
);

CREATE INDEX IF NOT EXISTS crafting_recipe_items_recipe_id_idx
    ON crafting_recipe_items (crafting_recipe_id);
