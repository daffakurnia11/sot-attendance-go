DROP TABLE IF EXISTS business_price_audit;
ALTER TABLE business_partner_prices DROP COLUMN IF EXISTS version;
ALTER TABLE business_partner_products DROP COLUMN IF EXISTS version;
