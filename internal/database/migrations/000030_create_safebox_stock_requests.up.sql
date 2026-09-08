CREATE TABLE IF NOT EXISTS safebox_stock_requests (
    idempotency_key TEXT PRIMARY KEY,
    actor_member_id BIGINT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT safebox_stock_requests_key_not_blank CHECK (BTRIM(idempotency_key) <> ''),
    CONSTRAINT safebox_stock_requests_actor_fkey FOREIGN KEY (actor_member_id)
        REFERENCES members (id) ON DELETE RESTRICT
);
