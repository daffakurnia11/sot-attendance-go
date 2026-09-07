-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Curated names that were left behind because they were ambiguous are not
-- recoverable from here - they were never copied out of members in the first
-- place, so this only unwinds the column.

ALTER TABLE server_members DROP COLUMN IF EXISTS character_name;
