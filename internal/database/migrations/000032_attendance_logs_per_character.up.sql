-- Attendance is recorded per character, not per member.
--
-- A member can hold several characters, and the family decided each one earns
-- its own attendance: one player logged 138 minutes on one character and 115 on
-- another in the same window, and a single row keyed on the member could only
-- say 253 and call it attended. Two rows say what actually happened - one met
-- the threshold, one did not.
--
-- server_member_id is nullable, because the presence fallback produces
-- attendance for members the game server has never reported. Those rows have no
-- character to name, so the unique index folds NULL to 0: a plain unique
-- constraint treats NULLs as distinct and would let the same window be written
-- twice for the same member.
--
-- Every statement is idempotent, which the startup runner requires: it
-- re-executes every *.up.sql on every boot.

ALTER TABLE attendance_logs ADD COLUMN IF NOT EXISTS server_member_id BIGINT;

ALTER TABLE attendance_logs
    DROP CONSTRAINT IF EXISTS attendance_logs_member_window_unique;

CREATE UNIQUE INDEX IF NOT EXISTS attendance_logs_member_character_window_unique
    ON attendance_logs (member_id, COALESCE(server_member_id, 0), attendance_start, attendance_end);

-- Deleting a character must not delete the attendance it earned: the window it
-- was recorded for still happened, and the payslip built on it still stands.
ALTER TABLE attendance_logs
    DROP CONSTRAINT IF EXISTS attendance_logs_server_member_foreign_key;
ALTER TABLE attendance_logs
    ADD CONSTRAINT attendance_logs_server_member_foreign_key
    FOREIGN KEY (server_member_id) REFERENCES server_members (id) ON DELETE SET NULL;
