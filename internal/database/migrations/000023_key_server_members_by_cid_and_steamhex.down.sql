-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Re-adding the foreign key fails if any member_id no longer points at a live
-- members row, which is exactly what dropping it allows.

DROP INDEX IF EXISTS server_members_cid_steamhex_unique;
CREATE UNIQUE INDEX IF NOT EXISTS server_members_license_cid_unique
    ON server_members (license_id, cid);
ALTER TABLE server_members ALTER COLUMN steamhex DROP NOT NULL;
ALTER TABLE server_members ADD CONSTRAINT server_members_member_id_fkey
    FOREIGN KEY (member_id) REFERENCES members (id) ON DELETE SET NULL;
