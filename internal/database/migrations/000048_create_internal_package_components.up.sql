-- Supplemental stock quantities per package; primary weapon is already on the price row.
CREATE TABLE IF NOT EXISTS business_internal_package_components (
    price_id BIGINT NOT NULL REFERENCES business_internal_prices(id) ON DELETE RESTRICT,
    stock_item_id BIGINT NOT NULL REFERENCES safebox_stock_items(id) ON DELETE RESTRICT,
    quantity BIGINT NOT NULL CHECK (quantity > 0 AND quantity <= 2147483647),
    PRIMARY KEY (price_id,stock_item_id)
);
INSERT INTO business_internal_package_components(price_id,stock_item_id,quantity)
SELECT p.id,i.id,v.quantity FROM business_internal_prices p
JOIN safebox_stock_items weapon ON weapon.id=p.stock_item_id
JOIN (VALUES
 ('desert_eagle','ammo_pistol',100),('revolver_mk2','ammo_pistol',100),
 ('mp9','ammo_smg',200),('vector','ammo_smg',300),('crx_mk2','ammo_rifle',300),
 ('desert_eagle','vest',2),('revolver_mk2','vest',2),('mp9','vest',2),('vector','vest',2),('crx_mk2','vest',2),
 ('desert_eagle','joint',2),('revolver_mk2','joint',2),('mp9','joint',2),('vector','joint',2),('crx_mk2','joint',2)
) v(weapon_key,item_key,quantity) ON v.weapon_key=weapon.item_key
JOIN safebox_stock_items i ON i.item_key=v.item_key
WHERE p.unit='package'
ON CONFLICT DO NOTHING;
