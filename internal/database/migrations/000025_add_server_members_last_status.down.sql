-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Dropping the column drops server_members_connected_idx with it. The values
-- are recoverable from server_logs, which remains the record.

ALTER TABLE server_members DROP COLUMN IF EXISTS last_status;
