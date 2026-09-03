-- Manual rollback only; the startup runner never executes a .down.sql file.
-- server_key comes back nullable: the original literal cannot be recovered,
-- and the payload no longer supplies one.

ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_event_id_is_body_hash;
DROP INDEX IF EXISTS server_logs_disconnected_session_idx;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS server_key TEXT;
