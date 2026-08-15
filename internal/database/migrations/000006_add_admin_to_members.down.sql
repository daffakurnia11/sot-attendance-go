DROP INDEX IF EXISTS members_is_admin_idx;
ALTER TABLE members DROP COLUMN IF EXISTS is_admin;
