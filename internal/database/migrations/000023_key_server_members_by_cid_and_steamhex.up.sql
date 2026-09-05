-- Make server_members independent of members, keyed on (cid, steamhex).
--
-- Three changes, all driven by what the FiveM data actually looks like:
--
-- 1. The member_id foreign key is dropped. server_members is now standalone
--    reference data: it records who the game server saw, whether or not that
--    person is a registered SOT member. member_id survives as a soft link,
--    still resolved from the Discord id, but deleting a member no longer
--    reaches into this table.
--
-- 2. Identity moves from (license_id, cid) to (cid, steamhex). A player can
--    hold several rows under one discord_user_id, and license can change, so
--    neither is a stable identity. The character id plus the Steam account is.
--
-- 3. steamhex becomes NOT NULL because it is now half the key. Left nullable,
--    two rows with a NULL steamhex would not collide and duplicates would
--    accumulate silently. Every existing row already has one, and the
--    validator has required it since the identifiers were tightened.
--
-- Every statement is idempotent, which the startup runner requires: it
-- re-executes every *.up.sql on every boot. ALTER COLUMN ... SET NOT NULL is a
-- no-op when the column is already NOT NULL.

ALTER TABLE server_members DROP CONSTRAINT IF EXISTS server_members_member_id_fkey;

ALTER TABLE server_members ALTER COLUMN steamhex SET NOT NULL;

DROP INDEX IF EXISTS server_members_license_cid_unique;

CREATE UNIQUE INDEX IF NOT EXISTS server_members_cid_steamhex_unique
    ON server_members (cid, steamhex);
