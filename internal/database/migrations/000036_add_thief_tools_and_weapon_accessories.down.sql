DELETE FROM safebox_stock_balances
WHERE item_id IN (
    SELECT id FROM safebox_stock_items
    WHERE item_key IN (
        'cannabis', 'coca', 'cannabis_bag', 'rolling_paper',
        'hacker_phone', 'thermite', 'usb', 'lockpick',
        'extended_smg_clip', 'tactical_suppressor', 'extended_mg_clip',
        'suppressor', 'extended_pistol_clip', 'macro_scope'
    )
);

DELETE FROM safebox_stock_items
WHERE item_key IN (
    'cannabis', 'coca', 'cannabis_bag', 'rolling_paper',
    'hacker_phone', 'thermite', 'usb', 'lockpick',
    'extended_smg_clip', 'tactical_suppressor', 'extended_mg_clip',
    'suppressor', 'extended_pistol_clip', 'macro_scope'
);

ALTER TABLE safebox_stock_items
    DROP CONSTRAINT IF EXISTS safebox_stock_items_group_valid;

ALTER TABLE safebox_stock_items
    ADD CONSTRAINT safebox_stock_items_group_valid CHECK (
        stock_group IN ('crafting', 'ammo', 'body_drugs', 'weapon', 'blueprint')
    );
