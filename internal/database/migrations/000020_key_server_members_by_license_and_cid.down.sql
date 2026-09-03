-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Will fail if any license already holds more than one character, which is the
-- situation this migration exists to allow.

DROP INDEX IF EXISTS server_members_license_cid_unique;
ALTER TABLE server_members ADD CONSTRAINT server_members_license_id_unique UNIQUE (license_id);
