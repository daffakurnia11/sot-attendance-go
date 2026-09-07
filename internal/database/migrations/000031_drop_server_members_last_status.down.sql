-- Manual rollback only; the startup runner never executes a .down.sql file.
-- The column comes back filled from server_logs, which is where the value was
-- derived from in the first place.

ALTER TABLE server_members ADD COLUMN IF NOT EXISTS last_status TEXT;

CREATE INDEX IF NOT EXISTS server_members_connected_idx
    ON server_members (last_status)
    WHERE last_status = 'connected';

UPDATE server_members sm
SET last_status = (
    SELECT sl.status
    FROM server_logs sl
    WHERE sl.server_member_id = sm.id
    ORDER BY sl.occurred_at DESC, sl.id DESC
    LIMIT 1
);
