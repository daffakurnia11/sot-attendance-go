-- Drop server_members.character_name.
--
-- 000026 moved the curated name here from members, on the reasoning that a
-- character name belongs to a character. Once it sat beside player_name, which
-- the webhook reports on every event, the two read as one value stored twice:
-- 15 of the 19 curated names were the reported name retyped by hand.
--
-- The remaining 4 were genuine overrides and are not recoverable from here.
-- That is the accepted cost: the character name is now whatever the game server
-- says it is, one source instead of two that drift apart silently.
--
-- The write path goes with the column. PATCH /api/v1/me/profile had only this
-- field left to set - cfx_name became read-only when 000027 dropped it - so the
-- endpoint is gone rather than left accepting values it would discard.
--
-- 000026 no longer contains a statement. ADD COLUMN IF NOT EXISTS is not a
-- no-op once a column is gone, and the startup runner re-executes every
-- *.up.sql on every boot, so replaying it would put the column back each time.

ALTER TABLE server_members DROP COLUMN IF EXISTS character_name;
