DROP INDEX IF EXISTS business_orders_buyer_idx;
ALTER TABLE business_orders DROP COLUMN IF EXISTS buyer_member_id;
