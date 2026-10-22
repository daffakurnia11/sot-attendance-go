ALTER TABLE business_price_audit DROP CONSTRAINT IF EXISTS business_price_audit_direction_check;
ALTER TABLE business_price_audit ADD CONSTRAINT business_price_audit_direction_check CHECK (direction IN ('purchase','sale','internal'));
