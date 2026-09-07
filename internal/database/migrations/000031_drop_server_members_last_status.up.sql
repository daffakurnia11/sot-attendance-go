-- Drop server_members.last_status.
--
-- 000025 added it to answer one question: how many players are connected right
-- now, for the count in the server log embed footer. That footer is gone, and
-- with it ConnectedPlayerCount, the only reader. What remained was a column
-- written on every ingested event and read by nothing - an extra UPDATE inside
-- the ingest transaction, buying nothing.
--
-- The question it answered is still answerable without it, from server_logs:
-- the newest row per player is the player's status. That is what the column was
-- a cache of, and a cache with no reader is not a cache.
--
-- Dropping the column drops server_members_connected_idx with it. 000025 no
-- longer contains a statement: it created that index standalone, so replaying
-- it after the drop would fail with "column last_status does not exist" on
-- every boot, and its ADD COLUMN IF NOT EXISTS would put the column back.

ALTER TABLE server_members DROP COLUMN IF EXISTS last_status;
