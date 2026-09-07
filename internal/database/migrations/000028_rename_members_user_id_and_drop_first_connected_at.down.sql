-- Manual rollback only; the startup runner never executes a .down.sql file.
-- first_connected_at comes back filled with the row's created_at, which is the
-- closest surviving answer: the real first-seen timestamps are gone.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'members'
          AND column_name = 'discord_user_id'
    ) THEN
        ALTER TABLE members RENAME COLUMN discord_user_id TO user_id;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'members_discord_user_id_unique') THEN
        ALTER TABLE members RENAME CONSTRAINT members_discord_user_id_unique TO members_user_id_unique;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'members_discord_user_id_not_blank') THEN
        ALTER TABLE members RENAME CONSTRAINT members_discord_user_id_not_blank TO members_user_id_not_blank;
    END IF;
END $$;

ALTER TABLE members ADD COLUMN IF NOT EXISTS first_connected_at TIMESTAMPTZ;
UPDATE members SET first_connected_at = created_at WHERE first_connected_at IS NULL;
ALTER TABLE members ALTER COLUMN first_connected_at SET NOT NULL;
CREATE INDEX IF NOT EXISTS members_first_connected_at_idx ON members (first_connected_at);
