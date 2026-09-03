-- Manual rollback only; the startup runner never executes a .down.sql file.
-- event_id comes back nullable: it was derived from the body, so it can be
-- recomputed as encode(digest(payload::text, 'sha256'), 'hex') only where the
-- byte-for-byte original body is still known, which it is not after a reload.

DROP INDEX IF EXISTS server_logs_payload_unique;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS event_id TEXT;
ALTER TABLE server_logs DROP COLUMN IF EXISTS payload;
