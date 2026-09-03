-- Replace the derived event_id with the request body itself.
--
-- payload keeps the exact event for debugging and logging, and takes over as
-- the idempotency key. UNIQUE on jsonb compares canonicalised content, so a
-- retry dedupes even when the sender re-serialises with different key order or
-- whitespace -- which a hash of the raw bytes could not do.
--
-- payload stays nullable: rows written before this migration have no body to
-- backfill, and NULLs do not collide in a unique index.
--
-- Every statement is idempotent because the startup runner re-executes every
-- *.up.sql on every boot.

ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS payload JSONB;

ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_event_id_is_body_hash;
ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_event_id_unique;
ALTER TABLE server_logs DROP COLUMN IF EXISTS event_id;

CREATE UNIQUE INDEX IF NOT EXISTS server_logs_payload_unique ON server_logs (payload);
