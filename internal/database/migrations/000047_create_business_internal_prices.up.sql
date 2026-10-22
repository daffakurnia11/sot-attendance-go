CREATE TABLE IF NOT EXISTS business_internal_prices (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    partner_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    stock_item_id BIGINT NOT NULL,
    price BIGINT NOT NULL,
    unit TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT business_internal_prices_partner_fkey FOREIGN KEY (partner_id)
        REFERENCES business_partners (id) ON DELETE RESTRICT,
    CONSTRAINT business_internal_prices_stock_item_fkey FOREIGN KEY (stock_item_id)
        REFERENCES safebox_stock_items (id) ON DELETE RESTRICT,
    CONSTRAINT business_internal_prices_name_not_blank CHECK (BTRIM(name) <> ''),
    CONSTRAINT business_internal_prices_unit_not_blank CHECK (BTRIM(unit) <> ''),
    CONSTRAINT business_internal_prices_price_nonnegative CHECK (price >= 0)
);

CREATE INDEX IF NOT EXISTS business_internal_prices_partner_idx ON business_internal_prices (partner_id);
CREATE INDEX IF NOT EXISTS business_internal_prices_stock_item_idx ON business_internal_prices (stock_item_id);

INSERT INTO business_partners(code,name) VALUES ('SOT','SOT Internal') ON CONFLICT (code) DO NOTHING;

INSERT INTO business_internal_prices(partner_id,name,stock_item_id,price,unit,notes)
SELECT p.id,v.name,i.id,v.price,v.unit,v.notes FROM (VALUES
('Desert Eagle','desert_eagle',5500,'piece',''),
('Revolver MK2','revolver_mk2',8000,'piece',''),
('MP9','mp9',8000,'piece',''),
('Vector','vector',8500,'piece',''),
('CRX MK2','crx_mk2',15500,'piece',''),
('Ammo Pistol','ammo_pistol',2000,'clip','80 rounds/clip.'),
('Ammo SMG','ammo_smg',2500,'clip','150 rounds/clip.'),
('Ammo Rifle','ammo_rifle',3500,'clip','180 rounds/clip.'),
('Body Armor','vest',3000,'piece',''),
('Joint','joint',500,'piece',''),
('Desert Eagle','desert_eagle',13500,'package','1 Desert Eagle: $5,000
100 Ammo Pistol: $2,500
2 Body Armor: $6,000
2 Joint: Free'),
('Python','revolver_mk2',16000,'package','1 Python: $7,500
100 Ammo Pistol: $2,500
2 Body Armor: $6,000
2 Joint: Free'),
('MP9','mp9',17500,'package','1 MP9: $7,500
200 Ammo SMG: $4,000
2 Body Armor: $6,000
2 Joint: Free'),
('Vector','vector',18500,'package','1 Vector: $8,000
300 Ammo SMG: $4,500
2 Body Armor: $6,000
2 Joint: Free'),
('CRX MK2','crx_mk2',27500,'package','1 CRX MK2: $15,000
300 Ammo Rifle: $6,500
2 Body Armor: $6,000
2 Joint: Free')
) AS v(name,item_key,price,unit,notes)
JOIN safebox_stock_items i ON i.item_key=v.item_key
JOIN business_partners p ON p.code='SOT'
WHERE NOT EXISTS (SELECT 1 FROM business_internal_prices b WHERE b.partner_id=p.id AND b.stock_item_id=i.id AND b.unit=v.unit);
