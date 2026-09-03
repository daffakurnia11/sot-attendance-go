-- Key player identity on (license_id, cid) instead of license_id alone.
--
-- One Rockstar account can hold several framework characters: same license,
-- same Steam, different cid. Keyed on license alone they collapsed into one
-- row whose cid and username were overwritten by whichever event arrived last,
-- so the earlier character disappeared.
--
-- One row per character now. Several rows may share a member_id, which is
-- already supported: there is deliberately no unique index on member_id.
--
-- cid is NOT NULL, so the composite index cannot be defeated by NULLs failing
-- to collide.
--
-- Both statements are idempotent, which the startup runner requires: it
-- re-executes every *.up.sql on every boot.

ALTER TABLE server_members DROP CONSTRAINT IF EXISTS server_members_license_id_unique;

CREATE UNIQUE INDEX IF NOT EXISTS server_members_license_cid_unique
    ON server_members (license_id, cid);
