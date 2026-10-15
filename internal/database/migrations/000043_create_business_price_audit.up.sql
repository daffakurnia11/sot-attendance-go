ALTER TABLE business_partner_products ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE business_partner_prices ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
CREATE TABLE IF NOT EXISTS business_price_audit (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 direction TEXT NOT NULL CHECK (direction IN ('purchase','sale')),
 entry_id BIGINT NOT NULL,
 actor_member_id BIGINT NOT NULL REFERENCES members(id) ON DELETE RESTRICT,
 before_value JSONB NOT NULL,
 after_value JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS business_price_audit_entry_idx ON business_price_audit(direction,entry_id,created_at DESC);
