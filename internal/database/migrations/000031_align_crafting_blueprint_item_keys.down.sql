UPDATE crafting_recipe_items
SET item_code = 'blueprint'
WHERE item_code IN (
    'blueprint_weapon',
    'blueprint_magnum',
    'blueprint_mp9',
    'blueprint_vector',
    'blueprint_rifle'
);
