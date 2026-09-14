-- New stock groups and the items in them.
-- Startup migration runner is re-runnable; keep all statements idempotent.

-- Thief tools and weapon accessories are held like any other stock, but neither
-- fits the five groups the catalog started with: they are not crafted, not
-- ammunition, not a weapon, not a blueprint, and not consumed by a character.
-- Rebuilt only when it does not already permit this migration's groups.
--
-- The runner replays every migration on each startup, and this rule is
-- rewritten by each migration that adds a group. Dropping and re-adding it
-- unconditionally meant a replay of an older migration narrowed the rule back
-- to the values it knew, which the rows a later migration had already inserted
-- then violated. Checking first makes the replay a no-op.
--
-- Dropped by what it checks rather than by what it is called: a database
-- restored from a TablePlus dump carries this rule under a generated name, and
-- naming only one of the two leaves the old value list in place.
DO $$
DECLARE
    existing TEXT;
    stale TEXT;
BEGIN
    SELECT pg_get_constraintdef(con.oid) INTO existing
    FROM pg_constraint con
    JOIN pg_class rel ON rel.oid = con.conrelid
    WHERE rel.relname = 'safebox_stock_items'
        AND con.contype = 'c'
        AND pg_get_constraintdef(con.oid) ILIKE '%stock_group%'
    LIMIT 1;

    IF existing IS NOT NULL
            AND existing LIKE '%thief_tools%'
            AND existing LIKE '%weapon_accessories%' THEN
        RETURN;
    END IF;

    FOR stale IN
        SELECT con.conname
        FROM pg_constraint con
        JOIN pg_class rel ON rel.oid = con.conrelid
        WHERE rel.relname = 'safebox_stock_items'
            AND con.contype = 'c'
            AND pg_get_constraintdef(con.oid) ILIKE '%stock_group%'
    LOOP
        EXECUTE format('ALTER TABLE safebox_stock_items DROP CONSTRAINT %I', stale);
    END LOOP;

    ALTER TABLE safebox_stock_items
        ADD CONSTRAINT safebox_stock_items_group_valid CHECK (
            stock_group IN ('crafting', 'ammo', 'body_drugs', 'weapon', 'blueprint', 'thief_tools', 'weapon_accessories')
        );
END $$;

INSERT INTO safebox_stock_items (item_key, name, stock_group)
VALUES
    ('cannabis', 'Cannabis', 'body_drugs'),
    ('coca', 'Coca', 'body_drugs'),
    ('cannabis_bag', 'Cannabis Bag', 'body_drugs'),
    ('rolling_paper', 'Rolling Paper', 'body_drugs'),
    ('hacker_phone', 'Hacker Phone', 'thief_tools'),
    ('thermite', 'Thermite', 'thief_tools'),
    ('usb', 'USB', 'thief_tools'),
    ('lockpick', 'Lockpick', 'thief_tools'),
    ('extended_smg_clip', 'Extended SMG Clip', 'weapon_accessories'),
    ('tactical_suppressor', 'Tactical Suppressor', 'weapon_accessories'),
    ('extended_mg_clip', 'Extended MG Clip', 'weapon_accessories'),
    ('suppressor', 'Suppressor', 'weapon_accessories'),
    ('extended_pistol_clip', 'Extended Pistol Clip', 'weapon_accessories'),
    ('macro_scope', 'Macro Scope', 'weapon_accessories')
ON CONFLICT (item_key) DO UPDATE SET
    name = EXCLUDED.name,
    stock_group = EXCLUDED.stock_group;

-- A row in both safeboxes so either can hold the item, carrying the counted
-- opening balance where one was given and zero in the other.
--
-- DO NOTHING, never DO UPDATE: this runs on every startup, and an update would
-- reset a live balance to its opening figure on each deploy. Correcting one of
-- these afterwards is the settings editor's job, not this file's.
INSERT INTO safebox_stock_balances (safebox, item_id, quantity)
SELECT safebox, item.id, seed.quantity
FROM (VALUES
    ('public', 'cannabis', 2204), ('public', 'coca', 8),
    -- Counted as present but with no figure given, so it opens at zero.
    ('public', 'cannabis_bag', 0), ('public', 'rolling_paper', 5),
    ('public', 'hacker_phone', 2), ('public', 'thermite', 3),
    ('public', 'usb', 10), ('public', 'lockpick', 10),
    ('public', 'extended_smg_clip', 0), ('public', 'tactical_suppressor', 0),
    ('public', 'extended_mg_clip', 0), ('public', 'suppressor', 0),
    ('public', 'extended_pistol_clip', 0), ('public', 'macro_scope', 0),
    ('boss', 'extended_smg_clip', 17), ('boss', 'tactical_suppressor', 14),
    ('boss', 'extended_mg_clip', 10), ('boss', 'suppressor', 14),
    ('boss', 'extended_pistol_clip', 13), ('boss', 'macro_scope', 2),
    ('boss', 'cannabis', 0), ('boss', 'coca', 0), ('boss', 'cannabis_bag', 0),
    ('boss', 'rolling_paper', 0), ('boss', 'hacker_phone', 0), ('boss', 'thermite', 0),
    ('boss', 'usb', 0), ('boss', 'lockpick', 0)
) AS seed(safebox, item_key, quantity)
JOIN safebox_stock_items item ON item.item_key = seed.item_key
ON CONFLICT (safebox, item_id) DO NOTHING;
