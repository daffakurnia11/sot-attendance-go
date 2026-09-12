DROP INDEX IF EXISTS server_logs_source_member_occurred_at_idx;

ALTER TABLE server_logs
    DROP CONSTRAINT IF EXISTS server_logs_source_valid;

ALTER TABLE server_logs
    DROP COLUMN IF EXISTS source;
