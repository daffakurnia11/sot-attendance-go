-- Manual rollback only; the startup runner never executes a .down.sql file.
-- The column comes back empty. The curated overrides are gone; player_name is
-- the only character name that survives, which is what reading it now does.

ALTER TABLE server_members ADD COLUMN IF NOT EXISTS character_name TEXT;
