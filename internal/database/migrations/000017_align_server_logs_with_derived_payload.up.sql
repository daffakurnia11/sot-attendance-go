-- Align server_logs with the payload the FiveM script actually sends.
--
-- The sender no longer supplies a server object, a session id, or an event id:
-- the backend derives the last two and there is one server.
--
-- Every statement is idempotent because the startup runner re-executes every
-- *.up.sql on every boot.

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

-- A third statement here once added a CHECK constraining event_id to a 64-hex
-- body hash. It is gone: 000019 drops both that constraint and the event_id
-- column, and the startup runner re-executes every up file on every boot, so
-- on the next boot the guard saw no constraint, tried to add it, and failed
-- with "column event_id does not exist".
--
-- The lesson for anything added later: a migration must be idempotent AND
-- survive a later migration removing what it references. Guard on the thing
-- you are about to touch, not only on the thing you are about to create.
