-- Manual seed: apply migration 000042 first, then run with psql -v ON_ERROR_STOP=1 -f this_file.
-- SOT sells to partners. Purchase catalogs, balances and existing prices stay untouched.
BEGIN;
-- Serialize concurrent runs, including the NOT EXISTS insert below.
LOCK TABLE business_partners, business_partner_prices IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (VALUES
            ('desert_eagle'), ('revolver_mk2'), ('mp9'), ('vector'), ('crx_mk2')
        ) required(item_key)
        WHERE NOT EXISTS (
            SELECT 1 FROM safebox_stock_items item WHERE item.item_key = required.item_key
        )
    ) THEN
        RAISE EXCEPTION 'Business partner seed requires all five weapon stock items';
    END IF;
END $$;

INSERT INTO business_partners (code, name, description)
VALUES
    ('GWF', 'GWF', ''),
    ('DTM', 'DON''T TOUCH ME', ''),
    ('BZ', 'BLADEZAY', ''),
    ('NOCTIS', 'NOCTIS', ''),
    ('DVG', 'DVG', ''),
    ('CR', 'CHAOS REBORN', ''),
    ('CN', 'COSA NOSTRA', ''),
    ('DW', 'DWARRIORS', '')
ON CONFLICT (code) DO NOTHING;

WITH seed(partner_code, item_key, name, price, notes) AS (
    VALUES
    ('GWF', 'desert_eagle', 'Desert Eagle', 6000, '25/week (shared)'),
    ('GWF', 'revolver_mk2', 'Revolver MK2', 7800, '25/week (shared)'),
    ('GWF', 'mp9', 'MP9', 9000, '25/week (shared)'),
    ('GWF', 'vector', 'Vector', 9000, '25/week (shared)'),
    ('GWF', 'crx_mk2', 'CRX MK2', 16000, '15/week (shared)'),
    ('DTM', 'desert_eagle', 'Desert Eagle', 6000, '20/week (shared)'),
    ('DTM', 'revolver_mk2', 'Revolver MK2', 9000, '20/week (shared)'),
    ('DTM', 'mp9', 'MP9', 9000, '20/week (shared)'),
    ('DTM', 'vector', 'Vector', 10000, '20/week (shared)'),
    ('DTM', 'crx_mk2', 'CRX MK2', 16500, '15/week (shared)'),
    ('BZ', 'desert_eagle', 'Desert Eagle', 5000, '20/week (shared)'),
    ('BZ', 'revolver_mk2', 'Revolver MK2', 7500, '20/week (shared)'),
    ('BZ', 'mp9', 'MP9', 7500, '20/week (shared)'),
    ('BZ', 'vector', 'Vector', 8000, '20/week (shared)'),
    ('BZ', 'crx_mk2', 'CRX MK2', 15000, '15/week (shared)'),
    ('NOCTIS', 'desert_eagle', 'Desert Eagle', 6000, '20/week (shared)'),
    ('NOCTIS', 'revolver_mk2', 'Revolver MK2', 8000, '20/week (shared)'),
    ('NOCTIS', 'mp9', 'MP9', 7500, '20/week (shared)'),
    ('NOCTIS', 'vector', 'Vector', 8000, '20/week (shared)'),
    ('NOCTIS', 'crx_mk2', 'CRX MK2', 15500, '15/week (shared)'),
    ('DVG', 'desert_eagle', 'Desert Eagle', 6000, '20/week (shared)'),
    ('DVG', 'revolver_mk2', 'Revolver MK2', 7800, '20/week (shared)'),
    ('DVG', 'mp9', 'MP9', 7800, '20/week (shared)'),
    ('DVG', 'vector', 'Vector', 8500, '20/week (shared)'),
    ('DVG', 'crx_mk2', 'CRX MK2', 16000, '10/week (shared)'),
    ('CR', 'desert_eagle', 'Desert Eagle', 6000, '20/week (shared)'),
    ('CR', 'revolver_mk2', 'Revolver MK2', 7700, '20/week (shared)'),
    ('CR', 'mp9', 'MP9', 7700, '20/week (shared)'),
    ('CR', 'vector', 'Vector', 8000, '20/week (shared)'),
    ('CR', 'crx_mk2', 'CRX MK2', 15000, '12/week (shared)'),
    ('CN', 'desert_eagle', 'Desert Eagle', 5500, '20/week (shared)'),
    ('CN', 'revolver_mk2', 'Revolver MK2', 7500, '20/week (shared)'),
    ('CN', 'mp9', 'MP9', 7500, '20/week (shared)'),
    ('CN', 'vector', 'Vector', 8000, '20/week (shared)'),
    ('CN', 'crx_mk2', 'CRX MK2', 15000, '10/week (shared)'),
    ('DW', 'desert_eagle', 'Desert Eagle', 6000, '100/week (shared)'),
    ('DW', 'revolver_mk2', 'Revolver MK2', 8500, '100/week (shared)'),
    ('DW', 'mp9', 'MP9', 7500, '100/week (shared)'),
    ('DW', 'vector', 'Vector', 10000, '100/week (shared)'),
    ('DW', 'crx_mk2', 'CRX MK2', 15000, '100/week (shared)')
)
INSERT INTO business_partner_prices (partner_id, name, stock_item_id, price, unit, notes)
SELECT partner.id, seed.name, item.id, seed.price, 'piece', seed.notes
FROM seed
JOIN business_partners partner ON partner.code = seed.partner_code
JOIN safebox_stock_items item ON item.item_key = seed.item_key
WHERE NOT EXISTS (
    SELECT 1 FROM business_partner_prices existing
    WHERE existing.partner_id = partner.id
        AND existing.stock_item_id = item.id
        AND existing.unit = 'piece'
);

COMMIT;
