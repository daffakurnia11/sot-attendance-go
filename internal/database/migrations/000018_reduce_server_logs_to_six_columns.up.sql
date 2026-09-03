-- Reduce server_logs to the six columns that carry meaning.
--
-- What each survivor is for:
--   id                a monotonic tiebreaker. The payload sends the same
--                     timestamp for every event of a visit, so ordering by
--                     occurred_at alone is ambiguous.
--   event_id          SHA-256 of the request body. The only thing stopping a
--                     retry from inserting a second row.
--   server_member_id  the link to the player, and through them to members.
--   session_id        groups one visit so playtime can be computed.
--   status            connecting / connected / disconnected.
--   occurred_at       event time, from event.timestamp.
--
-- Removed and how to get them back if ever needed:
--   connected_at      = MIN(occurred_at) per session_id
--   created_at        occurred_at already carries event time
--   player_name       latest value lives on server_members
--   ping              no reader
--   player_server_id  no reader
--   disconnect_reason no reader
--
-- DROP COLUMN IF EXISTS is idempotent, which the startup runner requires: it
-- re-executes every *.up.sql on every boot.

ALTER TABLE server_logs DROP COLUMN IF EXISTS connected_at;
ALTER TABLE server_logs DROP COLUMN IF EXISTS disconnect_reason;
ALTER TABLE server_logs DROP COLUMN IF EXISTS player_server_id;
ALTER TABLE server_logs DROP COLUMN IF EXISTS player_name;
ALTER TABLE server_logs DROP COLUMN IF EXISTS ping;
ALTER TABLE server_logs DROP COLUMN IF EXISTS created_at;
