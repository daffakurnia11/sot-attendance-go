ALTER TABLE business_orders ADD COLUMN IF NOT EXISTS buyer_member_id BIGINT REFERENCES members(id) ON DELETE RESTRICT;
CREATE INDEX IF NOT EXISTS business_orders_buyer_idx ON business_orders(buyer_member_id) WHERE buyer_member_id IS NOT NULL;
