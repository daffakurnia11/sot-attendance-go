-- Records where a server_logs row came from.
--
-- The FiveM webhook was the only writer, so every existing row is 'server' and
-- the default keeps it that way without a backfill. The Discord presence poller
-- writes 'discord': the CR Roleplay server drops connect events when its own
-- connection is poor, and a member's Discord activity is the second witness to
-- a visit the webhook never reported.
ALTER TABLE server_logs
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'server';

-- Added only when absent. The runner replays every migration on each boot and
-- 000039 widens this rule to admit 'cfx'; dropping and re-adding it here
-- unconditionally would narrow it back on every replay, and the 'cfx' rows
-- already stored would then violate it and abort startup.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'server_logs'::regclass AND conname = 'server_logs_source_valid'
    ) THEN
        ALTER TABLE server_logs
            ADD CONSTRAINT server_logs_source_valid CHECK (source IN ('server', 'discord'));
    END IF;
END $$;

-- The poller reads the open visit per character per source on every tick.
CREATE INDEX IF NOT EXISTS server_logs_source_member_occurred_at_idx
    ON server_logs (source, server_member_id, occurred_at DESC);
