-- Discord activity logs, one row per observed transition.
--
-- The table was created as player_logs. 000029 renamed it to activity_logs to
-- say which feed it carries: this one is Discord rich presence, watched through
-- the gateway, while server_logs carries what the CR Roleplay server itself
-- reported over the webhook. The two disagreeing is a signal worth seeing, so
-- they stay separate tables.
--
-- This file creates the table under its current name so a fresh database and a
-- renamed one converge. CREATE TABLE IF NOT EXISTS player_logs would otherwise
-- recreate an empty player_logs beside activity_logs on the next boot, since
-- the startup runner re-executes every *.up.sql on every boot.
--
-- The guard is what makes the upgrade path work. A database that predates the
-- rename still has player_logs holding every row, and this file runs before
-- 000029 gets to rename it. Creating activity_logs here unguarded left the data
-- stranded in player_logs while 000029, finding activity_logs already present,
-- skipped the rename and the application read an empty table. So: build the
-- table only when there is no player_logs to become it.

DO $$
BEGIN
    IF to_regclass('public.player_logs') IS NOT NULL THEN
        RETURN;
    END IF;

    CREATE TABLE IF NOT EXISTS activity_logs (
        id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
        member_id BIGINT NOT NULL,
        status TEXT NOT NULL,
        started_at TIMESTAMPTZ,
        occurred_at TIMESTAMPTZ NOT NULL,
        playtime INTERVAL,
        created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        CONSTRAINT activity_logs_member_foreign_key FOREIGN KEY (member_id)
            REFERENCES members (id) ON DELETE CASCADE,
        CONSTRAINT activity_logs_status_valid CHECK (
            status IN ('connecting', 'connected', 'disconnected')
        ),
        CONSTRAINT activity_logs_playtime_not_negative CHECK (
            playtime IS NULL OR playtime >= INTERVAL '0 seconds'
        )
    );

    CREATE INDEX IF NOT EXISTS activity_logs_member_id_occurred_at_idx
        ON activity_logs (member_id, occurred_at DESC);

    CREATE INDEX IF NOT EXISTS activity_logs_occurred_at_idx
        ON activity_logs (occurred_at DESC);
END $$;
