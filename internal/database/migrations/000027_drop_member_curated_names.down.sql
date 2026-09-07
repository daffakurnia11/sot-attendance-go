-- Manual rollback only; the startup runner never executes a .down.sql file.
-- The columns come back empty. character_name can be rebuilt from
-- server_members.character_name, cfx_name from server_members.username, which
-- is what reading through the join now does.

ALTER TABLE members ADD COLUMN IF NOT EXISTS character_name TEXT;
ALTER TABLE members ADD COLUMN IF NOT EXISTS cfx_name TEXT;

UPDATE members m
SET character_name = sm.character_name,
    cfx_name = sm.username
FROM server_members sm
WHERE sm.discord_user_id = m.discord_user_id
  AND m.character_name IS NULL;
