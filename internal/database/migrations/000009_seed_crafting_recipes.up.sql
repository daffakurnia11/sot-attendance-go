INSERT INTO crafting_recipes (
    weapon_code,
    weapon_name,
    output_quantity,
    crafting_time
)
VALUES
    ('desert_eagle', 'Desert Eagle (DE)', 1, INTERVAL '8 seconds'),
    ('revolver_mk2', 'Revolver MK2 (Python)', 1, INTERVAL '8 seconds'),
    ('mp9', 'MP9', 1, INTERVAL '8 seconds'),
    ('vector', 'Vector', 1, INTERVAL '8 seconds'),
    ('crx_mk2', 'CRX MK2', 1, INTERVAL '8 seconds')
ON CONFLICT (weapon_code) DO NOTHING;

INSERT INTO crafting_recipe_items (
    crafting_recipe_id,
    item_code,
    item_name,
    quantity
)
SELECT
    recipe.id,
    ingredient.item_code,
    ingredient.item_name,
    ingredient.quantity
FROM (
    VALUES
        ('desert_eagle', 'aluminium', 'Aluminium', 15),
        ('desert_eagle', 'metal_scrap', 'Metal Scrap', 15),
        ('desert_eagle', 'rubber', 'Rubber', 15),
        ('desert_eagle', 'blueprint', 'Blueprint Weapon', 1),
        ('desert_eagle', 'sulfur', 'Sulfur', 25),
        ('desert_eagle', 'copper', 'Copper', 25),
        ('desert_eagle', 'chicken_feather', 'Chicken Feather', 10),
        ('desert_eagle', 'iron', 'Iron', 25),
        ('revolver_mk2', 'aluminium', 'Aluminium', 20),
        ('revolver_mk2', 'metal_scrap', 'Metal Scrap', 20),
        ('revolver_mk2', 'rubber', 'Rubber', 20),
        ('revolver_mk2', 'blueprint', 'Blueprint Magnum', 1),
        ('revolver_mk2', 'sulfur', 'Sulfur', 40),
        ('revolver_mk2', 'copper', 'Copper', 40),
        ('revolver_mk2', 'chicken_feather', 'Chicken Feather', 15),
        ('revolver_mk2', 'iron', 'Iron', 40),
        ('mp9', 'aluminium', 'Aluminium', 20),
        ('mp9', 'copper', 'Copper', 40),
        ('mp9', 'rubber', 'Rubber', 20),
        ('mp9', 'metal_scrap', 'Metal Scrap', 20),
        ('mp9', 'sulfur', 'Sulfur', 40),
        ('mp9', 'blueprint', 'Blueprint Mp9', 1),
        ('mp9', 'chicken_feather', 'Chicken Feather', 15),
        ('mp9', 'iron', 'Iron', 40),
        ('vector', 'aluminium', 'Aluminium', 20),
        ('vector', 'metal_scrap', 'Metal Scrap', 20),
        ('vector', 'rubber', 'Rubber', 20),
        ('vector', 'blueprint', 'Blueprint Vector', 1),
        ('vector', 'sulfur', 'Sulfur', 40),
        ('vector', 'copper', 'Copper', 40),
        ('vector', 'chicken_feather', 'Chicken Feather', 15),
        ('vector', 'iron', 'Iron', 40),
        ('crx_mk2', 'chicken_feather', 'Chicken Feather', 25),
        ('crx_mk2', 'blueprint', 'Blueprint Rifle', 1),
        ('crx_mk2', 'copper', 'Copper', 40),
        ('crx_mk2', 'iron', 'Iron', 50),
        ('crx_mk2', 'metal_scrap', 'Metal Scrap', 40),
        ('crx_mk2', 'rubber', 'Rubber', 40),
        ('crx_mk2', 'sulfur', 'Sulfur', 50),
        ('crx_mk2', 'aluminium', 'Aluminium', 30)
) AS ingredient (weapon_code, item_code, item_name, quantity)
JOIN crafting_recipes AS recipe
    ON recipe.weapon_code = ingredient.weapon_code
ON CONFLICT (crafting_recipe_id, item_code) DO NOTHING;
