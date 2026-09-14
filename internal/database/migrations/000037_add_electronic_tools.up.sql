-- Electronic tools, held in the public stash.
-- Startup migration runner is re-runnable; keep all statements idempotent.

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
            AND existing LIKE '%electronic_tools%' THEN
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
            stock_group IN ('crafting', 'ammo', 'body_drugs', 'weapon', 'blueprint', 'thief_tools', 'weapon_accessories', 'electronic_tools')
        );
END $$;

INSERT INTO safebox_stock_items (item_key, name, stock_group)
VALUES
    ('phone_box', 'Phone Box', 'electronic_tools'),
    ('radio_box', 'Radio Box', 'electronic_tools'),
    ('powerbank_box', 'Powerbank Box', 'electronic_tools')
ON CONFLICT (item_key) DO UPDATE SET
    name = EXCLUDED.name,
    stock_group = EXCLUDED.stock_group;

-- A row in both safeboxes so either can hold the item, carrying the counted
-- opening balance in the public stash and zero in the boss one.
--
-- DO NOTHING, never DO UPDATE: this runs on every startup, and an update would
-- reset a live balance to its opening figure on each deploy.
INSERT INTO safebox_stock_balances (safebox, item_id, quantity)
SELECT safebox, item.id, seed.quantity
FROM (VALUES
    ('public', 'phone_box', 9), ('public', 'radio_box', 9), ('public', 'powerbank_box', 2),
    ('boss', 'phone_box', 0), ('boss', 'radio_box', 0), ('boss', 'powerbank_box', 0)
) AS seed(safebox, item_key, quantity)
JOIN safebox_stock_items item ON item.item_key = seed.item_key
ON CONFLICT (safebox, item_id) DO NOTHING;
