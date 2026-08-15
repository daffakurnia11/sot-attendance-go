ALTER TABLE members
    ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS members_is_admin_idx ON members (is_admin) WHERE is_admin;
