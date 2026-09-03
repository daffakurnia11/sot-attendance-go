-- Align server_logs with the payload the FiveM script actually sends.
--
-- The sender no longer supplies a server object, a session id, or an event id:
-- the backend derives the last two and there is one server. Three consequences
-- for this table.
--
-- Every statement is idempotent because the startup runner re-executes every
-- *.up.sql on every boot. ALTER TABLE ADD CONSTRAINT has no IF NOT EXISTS, so
-- the check below is guarded by a catalogue lookup instead.

-- 1. Session correlation is now on the hot path. resolveSession asks "which
--    session of this player has no disconnected event yet" for every connected
--    and disconnected event, and that anti-join probes by (session_id, status).
--    Without this partial index it degrades to a scan as the table grows.
CREATE INDEX IF NOT EXISTS server_logs_disconnected_session_idx
    ON server_logs (session_id)
    WHERE status = 'disconnected';

-- 2. server_key held the same literal on every row. The payload carries no
--    server object, so it could never vary, and no query filtered on it.
ALTER TABLE server_logs DROP COLUMN IF EXISTS server_key;

-- 3. event_id is now the SHA-256 of the request body, so it is always 64
--    lowercase hex characters. Enforcing that shape catches a regression where
--    the id stops being derived from the body, which would silently break
--    idempotency rather than fail loudly.
--
--    NOT VALID so the rows written before this change are left alone; new and
--    updated rows are checked.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'server_logs_event_id_is_body_hash'
          AND conrelid = 'server_logs'::regclass
    ) THEN
        ALTER TABLE server_logs
            ADD CONSTRAINT server_logs_event_id_is_body_hash
            CHECK (event_id ~ '^[0-9a-f]{64}$') NOT VALID;
    END IF;
END $$;
