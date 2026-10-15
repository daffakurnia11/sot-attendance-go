ALTER TABLE business_orders
    ADD COLUMN IF NOT EXISTS updated_by_member_id BIGINT REFERENCES members(id) ON DELETE RESTRICT;
