UPDATE crafting_recipe_items
SET item_code = CASE LOWER(item_name)
    WHEN 'blueprint weapon' THEN 'blueprint_weapon'
    WHEN 'blueprint magnum' THEN 'blueprint_magnum'
    WHEN 'blueprint mp9' THEN 'blueprint_mp9'
    WHEN 'blueprint vector' THEN 'blueprint_vector'
    WHEN 'blueprint rifle' THEN 'blueprint_rifle'
    ELSE item_code
END
WHERE item_code = 'blueprint';
