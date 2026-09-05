-- Track each player's latest status on their identity row.
--
-- The footer count was open visits: sessions with no disconnected event. That
-- overcounts, because a visit whose disconnected event never arrives - server
-- crash, lost sender queue - stays open forever and keeps being counted. Asking
-- how many players are currently connected is both the question being answered
-- and immune to that.
--
-- last_status is written from the newest stored event for the player, not from
-- the event being ingested, so an out-of-order connecting cannot overwrite a
-- later connected. Ingestion is order-independent by design and this has to
-- stay that way.
--
-- Nullable: rows written before this migration have no status until their next
-- event, and a NULL simply is not counted.
--
-- ADD COLUMN IF NOT EXISTS is idempotent, which the startup runner requires:
-- it re-executes every *.up.sql on every boot.

ALTER TABLE server_members ADD COLUMN IF NOT EXISTS last_status TEXT;

CREATE INDEX IF NOT EXISTS server_members_connected_idx
    ON server_members (last_status)
    WHERE last_status = 'connected';
