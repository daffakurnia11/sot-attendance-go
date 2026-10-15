CREATE TABLE IF NOT EXISTS business_order_notifications (
    order_id BIGINT NOT NULL REFERENCES business_orders(id) ON DELETE RESTRICT,
    destination TEXT NOT NULL CHECK (destination IN ('office', 'boss')),
    embed JSONB NOT NULL,
    delivered_message_id TEXT,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (order_id, destination)
);
CREATE INDEX IF NOT EXISTS business_order_notifications_pending_idx
    ON business_order_notifications(next_attempt_at) WHERE delivered_message_id IS NULL;
