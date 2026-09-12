-- Records where a server_logs row came from.
--
-- The FiveM webhook was the only writer, so every existing row is 'server' and
-- the default keeps it that way without a backfill. The Discord presence poller
-- writes 'discord': the CR Roleplay server drops connect events when its own
-- connection is poor, and a member's Discord activity is the second witness to
-- a visit the webhook never reported.
ALTER TABLE server_logs
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'server';

ALTER TABLE server_logs
    DROP CONSTRAINT IF EXISTS server_logs_source_valid;

ALTER TABLE server_logs
    ADD CONSTRAINT server_logs_source_valid CHECK (source IN ('server', 'discord'));

-- The poller reads the open visit per character per source on every tick.
CREATE INDEX IF NOT EXISTS server_logs_source_member_occurred_at_idx
    ON server_logs (source, server_member_id, occurred_at DESC);
