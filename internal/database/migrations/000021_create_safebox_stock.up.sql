-- Safebox stock catalog, balances, and audit history.
-- Startup migration runner is re-runnable; keep all statements idempotent.

CREATE TABLE IF NOT EXISTS safebox_stock_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_key TEXT NOT NULL,
    name TEXT NOT NULL,
    stock_group TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT safebox_stock_items_item_key_unique UNIQUE (item_key),
    CONSTRAINT safebox_stock_items_group_valid CHECK (
        stock_group IN ('crafting', 'ammo', 'body_drugs', 'weapon', 'blueprint')
    ),
    CONSTRAINT safebox_stock_items_name_not_blank CHECK (BTRIM(name) <> '')
);

CREATE TABLE IF NOT EXISTS safebox_stock_balances (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    safebox TEXT NOT NULL,
    item_id BIGINT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT safebox_stock_balances_safebox_valid CHECK (
        safebox IN ('public', 'boss')
    ),
    CONSTRAINT safebox_stock_balances_quantity_nonnegative CHECK (quantity >= 0),
    CONSTRAINT safebox_stock_balances_safebox_item_unique UNIQUE (safebox, item_id),
    CONSTRAINT safebox_stock_balances_item_fkey FOREIGN KEY (item_id)
        REFERENCES safebox_stock_items (id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS safebox_stock_balances_safebox_idx
    ON safebox_stock_balances (safebox, updated_at DESC);

CREATE TABLE IF NOT EXISTS safebox_stock_transactions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    safebox TEXT NOT NULL,
    item_id BIGINT NOT NULL,
    action TEXT NOT NULL,
    quantity_before INTEGER NOT NULL,
    quantity_after INTEGER NOT NULL,
    delta INTEGER NOT NULL,
    reason TEXT NOT NULL,
    actor_member_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT safebox_stock_transactions_safebox_valid CHECK (
        safebox IN ('public', 'boss')
    ),
    CONSTRAINT safebox_stock_transactions_action_valid CHECK (
        action IN ('adjustment', 'correction', 'reset')
    ),
    CONSTRAINT safebox_stock_transactions_quantities_nonnegative CHECK (
        quantity_before >= 0 AND quantity_after >= 0
    ),
    CONSTRAINT safebox_stock_transactions_delta_valid CHECK (
        delta = quantity_after - quantity_before
    ),
    CONSTRAINT safebox_stock_transactions_reason_not_blank CHECK (BTRIM(reason) <> ''),
    CONSTRAINT safebox_stock_transactions_item_fkey FOREIGN KEY (item_id)
        REFERENCES safebox_stock_items (id) ON DELETE RESTRICT,
    CONSTRAINT safebox_stock_transactions_actor_fkey FOREIGN KEY (actor_member_id)
        REFERENCES members (id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS safebox_stock_transactions_lookup_idx
    ON safebox_stock_transactions (safebox, item_id, created_at DESC, id DESC);

INSERT INTO safebox_stock_items (item_key, name, stock_group)
VALUES
    ('copper', 'Copper', 'crafting'),
    ('iron', 'Iron', 'crafting'),
    ('sulfur', 'Sulfur', 'crafting'),
    ('chicken_feather', 'Chicken Feather', 'crafting'),
    ('aluminium', 'Aluminium', 'crafting'),
    ('rubber', 'Rubber', 'crafting'),
    ('metal_scrap', 'Metal Scrap', 'crafting'),
    ('stone', 'Stone', 'crafting'),
    ('material_parts', 'Material Parts', 'crafting'),
    ('vest', 'Vest', 'body_drugs'),
    ('joint', 'Joint', 'body_drugs'),
    ('lsd', 'LSD', 'body_drugs'),
    ('ammo_karet', 'Ammo Karet', 'ammo'),
    ('ammo_pistol', 'Ammo Pistol', 'ammo'),
    ('ammo_smg', 'Ammo SMG', 'ammo'),
    ('ammo_rifle', 'Ammo Rifle', 'ammo'),
    ('pistol', 'Pistol Karet', 'weapon'),
    ('desert_eagle', 'Desert Eagle', 'weapon'),
    ('revolver_mk2', 'Revolver MK2', 'weapon'),
    ('mp9', 'MP9', 'weapon'),
    ('vector', 'Vector', 'weapon'),
    ('crx_mk2', 'CRX MK2', 'weapon'),
    ('blueprint_weapon', 'Blueprint Weapon', 'blueprint'),
    ('blueprint_magnum', 'Blueprint Magnum', 'blueprint'),
    ('blueprint_mp9', 'Blueprint MP9', 'blueprint'),
    ('blueprint_vector', 'Blueprint Vector', 'blueprint'),
    ('blueprint_rifle', 'Blueprint Rifle', 'blueprint')
ON CONFLICT (item_key) DO UPDATE SET
    name = EXCLUDED.name,
    stock_group = EXCLUDED.stock_group;

INSERT INTO safebox_stock_balances (safebox, item_id, quantity)
SELECT safebox, item.id, seed.quantity
FROM (VALUES
    ('public', 'copper', 7346), ('public', 'iron', 6074), ('public', 'sulfur', 6915),
    ('public', 'chicken_feather', 9050), ('public', 'aluminium', 8280), ('public', 'rubber', 5130),
    ('public', 'metal_scrap', 5572), ('public', 'stone', 3093), ('public', 'material_parts', 750),
    ('public', 'vest', 100), ('public', 'joint', 100), ('public', 'lsd', 12),
    ('public', 'ammo_karet', 467), ('public', 'ammo_pistol', 2000), ('public', 'ammo_smg', 6150),
    ('public', 'ammo_rifle', 4000), ('public', 'pistol', 5), ('public', 'desert_eagle', 0),
    ('public', 'revolver_mk2', 10), ('public', 'mp9', 11), ('public', 'vector', 10), ('public', 'crx_mk2', 10),
    ('public', 'blueprint_weapon', 0), ('public', 'blueprint_magnum', 0), ('public', 'blueprint_mp9', 0),
    ('public', 'blueprint_vector', 0), ('public', 'blueprint_rifle', 0),
    ('boss', 'copper', 0), ('boss', 'iron', 0), ('boss', 'sulfur', 0), ('boss', 'chicken_feather', 0),
    ('boss', 'aluminium', 0), ('boss', 'rubber', 0), ('boss', 'metal_scrap', 0), ('boss', 'vest', 94),
    ('boss', 'joint', 143), ('boss', 'ammo_karet', 0), ('boss', 'ammo_pistol', 5124),
    ('boss', 'ammo_smg', 5611), ('boss', 'ammo_rifle', 7150), ('boss', 'pistol', 0),
    ('boss', 'desert_eagle', 50), ('boss', 'revolver_mk2', 47), ('boss', 'mp9', 45),
    ('boss', 'vector', 37), ('boss', 'crx_mk2', 29), ('boss', 'blueprint_weapon', 3),
    ('boss', 'blueprint_magnum', 50), ('boss', 'blueprint_mp9', 50), ('boss', 'blueprint_vector', 50),
    ('boss', 'blueprint_rifle', 65)
) AS seed(safebox, item_key, quantity)
JOIN safebox_stock_items item ON item.item_key = seed.item_key
ON CONFLICT (safebox, item_id) DO NOTHING;
