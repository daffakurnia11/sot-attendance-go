-- Manual rollback only; the startup runner never executes a .down.sql file.
-- The column comes back empty: the stored links are not recoverable, though
-- they can be rebuilt from discord_user_id, which is what deriving them does.

ALTER TABLE server_members ADD COLUMN IF NOT EXISTS member_id BIGINT;
CREATE INDEX IF NOT EXISTS server_members_member_id_idx ON server_members (member_id);
UPDATE server_members sm
SET member_id = m.id
FROM members m
WHERE sm.member_id IS NULL AND sm.discord_user_id IS NOT NULL AND m.user_id = sm.discord_user_id;
