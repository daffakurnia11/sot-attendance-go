-- Name the Discord id the way the rest of the schema names it, and retire
-- first_connected_at.
--
-- members.user_id held a Discord user id, and server_members already called the
-- same value discord_user_id. Readers joined the two as
-- members.user_id = server_members.discord_user_id, which reads as though two
-- different things are being compared. One name for one value removes that.
--
-- first_connected_at recorded when a member was first seen, was written only by
-- the presence logger, and nothing displayed it. It is dropped rather than left
-- as an unread NOT NULL column that every insert had to supply. The values are
-- not recoverable, so 000001's index on it was removed at the same time; a
-- standalone CREATE INDEX naming a dropped column fails on every replay.
--
-- Every statement is guarded on the object it touches, since the startup runner
-- re-executes every *.up.sql on every boot. RENAME COLUMN has no IF EXISTS
-- form, so the guard is explicit.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'members'
          AND column_name = 'user_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'members'
          AND column_name = 'discord_user_id'
    ) THEN
        ALTER TABLE members RENAME COLUMN user_id TO discord_user_id;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'members_user_id_unique'
    ) THEN
        ALTER TABLE members
            RENAME CONSTRAINT members_user_id_unique TO members_discord_user_id_unique;
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'members_user_id_not_blank'
    ) THEN
        ALTER TABLE members
            RENAME CONSTRAINT members_user_id_not_blank TO members_discord_user_id_not_blank;
    END IF;
END $$;

ALTER TABLE members DROP COLUMN IF EXISTS first_connected_at;
