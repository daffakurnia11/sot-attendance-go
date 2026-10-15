-- Manual seed: apply migration 000042 first; run with psql -v ON_ERROR_STOP=1 -f this_file.
-- Partner sells to SOT. Clip contents are descriptive notes, not automated stock conversion.
BEGIN;
LOCK TABLE business_partners, business_partner_products IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (VALUES
            ('ammo_pistol'),
            ('ammo_rifle'),
            ('ammo_smg'),
            ('blueprint_magnum'),
            ('blueprint_mp9'),
            ('blueprint_rifle'),
            ('blueprint_vector'),
            ('blueprint_weapon'),
            ('joint'),
            ('vest')
        ) required(item_key)
        WHERE NOT EXISTS (
            SELECT 1 FROM safebox_stock_items item WHERE item.item_key = required.item_key
        )
    ) THEN
        RAISE EXCEPTION 'Partner purchase seed requires all referenced stock items';
    END IF;
END $$;

INSERT INTO business_partners (code, name, description)
VALUES
    ('GWF', 'GWF', ''),
    ('DTM', 'DON''T TOUCH ME', ''),
    ('BZ', 'BLADEZAY', ''),
    ('CR', 'CHAOS REBORN', ''),
    ('NOCTIS', 'NOCTIS', ''),
    ('DVG', 'DVG', ''),
    ('DW', 'DWARRIORS', '')
ON CONFLICT (code) DO NOTHING;

WITH seed(partner_code, item_key, name, price, unit, notes) AS (
    VALUES
    ('GWF', 'vest', 'Vest', 2800, 'piece', 'Source: 2026-10-09 update. Quantity unlimited.'),
    ('DTM', 'vest', 'Vest', 2800, 'piece', 'Source: 2026-10-02 update. Minimum order: 100. Earlier quote stated 2–3 days; lead time not reconfirmed in update.'),
    ('BZ', 'ammo_pistol', 'Ammo Pistol', 1700, 'clip', '80 rounds/clip. Weekly maximum unspecified.'),
    ('BZ', 'ammo_smg', 'Ammo SMG', 2200, 'clip', '150 rounds/clip. Weekly maximum unspecified.'),
    ('BZ', 'ammo_rifle', 'Ammo Rifle', 3100, 'clip', '180 rounds/clip. Weekly maximum unspecified.'),
    ('CR', 'blueprint_weapon', 'Blueprint Desert Eagle', 2000, 'piece', 'Quantity unrestricted while materials available. Same-day completion possible; scheduled orders supported.'),
    ('CR', 'blueprint_magnum', 'Blueprint Revolver MK2', 3000, 'piece', 'Quantity unrestricted while materials available. Same-day completion possible; scheduled orders supported.'),
    ('CR', 'blueprint_mp9', 'Blueprint MP9', 3000, 'piece', 'Quantity unrestricted while materials available. Same-day completion possible; scheduled orders supported.'),
    ('CR', 'blueprint_vector', 'Blueprint Vector', 3500, 'piece', 'Quantity unrestricted while materials available. Same-day completion possible; scheduled orders supported.'),
    ('CR', 'blueprint_rifle', 'Blueprint CRX MK2', 6000, 'piece', 'Quantity unrestricted while materials available. Same-day completion possible; scheduled orders supported.'),
    ('NOCTIS', 'joint', 'Joint', 500, 'piece', 'Partner sells Joint to SOT; direction confirmed.'),
    ('DVG', 'joint', 'Joint', 500, 'piece', 'Partner sells Joint to SOT; direction confirmed.'),
    ('DW', 'joint', 'Joint', 500, 'piece', 'Partner sells Joint to SOT; direction confirmed.')
)
INSERT INTO business_partner_products (partner_id, name, stock_item_id, price, unit, notes)
SELECT partner.id, seed.name, item.id, seed.price, seed.unit, seed.notes
FROM seed
JOIN business_partners partner ON partner.code = seed.partner_code
JOIN safebox_stock_items item ON item.item_key = seed.item_key
WHERE NOT EXISTS (
    SELECT 1 FROM business_partner_products existing
    WHERE existing.partner_id = partner.id
        AND existing.stock_item_id = item.id
        AND existing.unit = seed.unit
);

COMMIT;
