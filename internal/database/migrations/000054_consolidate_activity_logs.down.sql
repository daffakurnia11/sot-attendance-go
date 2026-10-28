-- Restore the physical legacy table before removing its archived server events.
BEGIN;
CREATE TABLE activity_logs_restored (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 member_id BIGINT NOT NULL REFERENCES members(id) ON DELETE CASCADE,
 status TEXT NOT NULL CHECK(status IN ('connecting','connected','disconnected')),
 started_at TIMESTAMPTZ,occurred_at TIMESTAMPTZ NOT NULL,playtime INTERVAL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),CHECK(playtime IS NULL OR playtime>=interval '0')
);
INSERT INTO activity_logs_restored OVERRIDING SYSTEM VALUE SELECT * FROM activity_logs;
DROP VIEW activity_logs;
DROP FUNCTION IF EXISTS write_legacy_activity();
DROP SEQUENCE IF EXISTS legacy_activity_id_seq;
ALTER TABLE activity_logs_restored RENAME TO activity_logs;
SELECT setval(pg_get_serial_sequence('activity_logs','id'),GREATEST(COALESCE((SELECT max(id) FROM activity_logs),0)+1,1),false);
CREATE INDEX activity_logs_member_id_occurred_at_idx ON activity_logs(member_id,occurred_at DESC);
CREATE INDEX activity_logs_occurred_at_idx ON activity_logs(occurred_at DESC);
DELETE FROM server_logs WHERE payload ? 'legacy_activity';
DROP INDEX IF EXISTS server_logs_legacy_activity_id_idx;
DROP INDEX IF EXISTS server_logs_legacy_member_time_idx;
ALTER TABLE server_logs ALTER COLUMN server_member_id SET NOT NULL;
ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_identity_required;
COMMIT;
