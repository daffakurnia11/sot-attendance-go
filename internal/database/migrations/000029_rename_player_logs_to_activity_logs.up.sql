-- Rename player_logs to activity_logs.
--
-- Two feeds record the same shape of event from different sources: this table
-- is Discord rich presence, watched through the gateway, and server_logs is
-- what the CR Roleplay server reported over the webhook. "player_logs" said
-- nothing about which, and the pairing read as though server_logs were a
-- variant of it. They stay separate tables - the two disagreeing is the signal
-- the webhook feature exists to surface - so the names have to carry the
-- distinction.
--
-- Nothing is deleted: ALTER TABLE ... RENAME keeps every row, and the indexes
-- and constraints are renamed alongside so a renamed database and a fresh one
-- built by 000003 end up identical.
--
-- 000003 now creates the table under the new name, so this file only has work
-- to do on a database that predates it. Every statement is guarded on the
-- object it touches, since the startup runner re-executes every *.up.sql on
-- every boot and RENAME has no IF EXISTS form. The constraint guards match on
-- the owning table as well as the name: a bare name match fired against a
-- constraint still sitting on player_logs and failed the whole boot.

DO $$
BEGIN
    IF to_regclass('public.player_logs') IS NOT NULL
       AND to_regclass('public.activity_logs') IS NULL THEN
        ALTER TABLE player_logs RENAME TO activity_logs;
    END IF;

    IF to_regclass('public.player_logs_member_id_occurred_at_idx') IS NOT NULL
       AND to_regclass('public.activity_logs_member_id_occurred_at_idx') IS NULL THEN
        ALTER INDEX player_logs_member_id_occurred_at_idx
            RENAME TO activity_logs_member_id_occurred_at_idx;
    END IF;

    IF to_regclass('public.player_logs_occurred_at_idx') IS NOT NULL
       AND to_regclass('public.activity_logs_occurred_at_idx') IS NULL THEN
        ALTER INDEX player_logs_occurred_at_idx
            RENAME TO activity_logs_occurred_at_idx;
    END IF;

    -- ALTER TABLE ... RENAME does not touch the sequence behind an identity
    -- column, so a renamed database kept player_logs_id_seq while a fresh one
    -- built by 000003 got activity_logs_id_seq. Same schema, two names: a later
    -- migration naming the sequence would work on one and fail on the other.
    IF to_regclass('public.player_logs_id_seq') IS NOT NULL
       AND to_regclass('public.activity_logs_id_seq') IS NULL THEN
        ALTER SEQUENCE player_logs_id_seq RENAME TO activity_logs_id_seq;
    END IF;

    IF to_regclass('public.player_logs_pkey') IS NOT NULL
       AND to_regclass('public.activity_logs_pkey') IS NULL THEN
        ALTER INDEX player_logs_pkey RENAME TO activity_logs_pkey;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'activity_logs' AND c.conname = 'player_logs_member_foreign_key'
    ) THEN
        ALTER TABLE activity_logs
            RENAME CONSTRAINT player_logs_member_foreign_key TO activity_logs_member_foreign_key;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'activity_logs' AND c.conname = 'player_logs_status_valid'
    ) THEN
        ALTER TABLE activity_logs
            RENAME CONSTRAINT player_logs_status_valid TO activity_logs_status_valid;
    END IF;

    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_class t ON t.oid = c.conrelid
        WHERE t.relname = 'activity_logs' AND c.conname = 'player_logs_playtime_not_negative'
    ) THEN
        ALTER TABLE activity_logs
            RENAME CONSTRAINT player_logs_playtime_not_negative TO activity_logs_playtime_not_negative;
    END IF;
END $$;
