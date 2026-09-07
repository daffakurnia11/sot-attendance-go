-- Manual rollback only; the startup runner never executes a .down.sql file.
-- The rename is reversible and loses nothing.

DO $$
BEGIN
    IF to_regclass('public.activity_logs') IS NOT NULL
       AND to_regclass('public.player_logs') IS NULL THEN
        ALTER TABLE activity_logs RENAME TO player_logs;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activity_logs_member_foreign_key') THEN
        ALTER TABLE player_logs
            RENAME CONSTRAINT activity_logs_member_foreign_key TO player_logs_member_foreign_key;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activity_logs_status_valid') THEN
        ALTER TABLE player_logs
            RENAME CONSTRAINT activity_logs_status_valid TO player_logs_status_valid;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'activity_logs_playtime_not_negative') THEN
        ALTER TABLE player_logs
            RENAME CONSTRAINT activity_logs_playtime_not_negative TO player_logs_playtime_not_negative;
    END IF;
    IF to_regclass('public.activity_logs_member_id_occurred_at_idx') IS NOT NULL THEN
        ALTER INDEX activity_logs_member_id_occurred_at_idx
            RENAME TO player_logs_member_id_occurred_at_idx;
    END IF;
    IF to_regclass('public.activity_logs_occurred_at_idx') IS NOT NULL THEN
        ALTER INDEX activity_logs_occurred_at_idx RENAME TO player_logs_occurred_at_idx;
    END IF;
    IF to_regclass('public.activity_logs_pkey') IS NOT NULL THEN
        ALTER INDEX activity_logs_pkey RENAME TO player_logs_pkey;
    END IF;
    IF to_regclass('public.activity_logs_id_seq') IS NOT NULL
       AND to_regclass('public.player_logs_id_seq') IS NULL THEN
        ALTER SEQUENCE activity_logs_id_seq RENAME TO player_logs_id_seq;
    END IF;
END $$;
