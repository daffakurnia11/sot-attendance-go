-- Server identities earn attendance even without an authentication account.
ALTER TABLE attendance_logs ALTER COLUMN member_id DROP NOT NULL;
ALTER TABLE attendance_logs DROP CONSTRAINT IF EXISTS attendance_logs_member_foreign_key;
ALTER TABLE attendance_logs ADD CONSTRAINT attendance_logs_member_foreign_key
    FOREIGN KEY (member_id) REFERENCES members (id) ON DELETE SET NULL;
DROP INDEX IF EXISTS attendance_logs_member_character_window_unique;
CREATE UNIQUE INDEX attendance_logs_member_character_window_unique
    ON attendance_logs (COALESCE(server_member_id, -member_id), attendance_start, attendance_end);

-- A character keeps the same window identity if its owner logs in later.
-- Negative auth ids retain legacy rows that had no server character.
