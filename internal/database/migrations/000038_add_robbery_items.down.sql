DELETE FROM safebox_stock_balances
WHERE item_id IN (
    SELECT id FROM safebox_stock_items
    WHERE item_key IN ('diamond_ring', 'golden_watch', 'golden_chain')
);

DELETE FROM safebox_stock_items
WHERE item_key IN ('diamond_ring', 'golden_watch', 'golden_chain');

ALTER TABLE safebox_stock_items
    DROP CONSTRAINT IF EXISTS safebox_stock_items_group_valid;

ALTER TABLE safebox_stock_items
    ADD CONSTRAINT safebox_stock_items_group_valid CHECK (
        stock_group IN (
            'crafting', 'ammo', 'body_drugs', 'weapon', 'blueprint',
            'thief_tools', 'weapon_accessories', 'electronic_tools'
        )
    );
