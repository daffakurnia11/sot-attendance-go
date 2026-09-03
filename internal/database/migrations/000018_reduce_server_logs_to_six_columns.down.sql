-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Columns come back nullable and empty: the dropped values are unrecoverable.

ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS connected_at TIMESTAMPTZ;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS disconnect_reason TEXT;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS player_server_id INTEGER;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS player_name TEXT;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS ping INTEGER;
ALTER TABLE server_logs ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
