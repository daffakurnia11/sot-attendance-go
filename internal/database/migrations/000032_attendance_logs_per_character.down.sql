-- Manual rollback only; the startup runner never executes a .down.sql file.
--
-- Rows written per character collide once the key is per member again, so the
-- newest row for each member and window survives and the rest are dropped. The
-- playtime they held is not merged back: it was never stored as a member total.

DELETE FROM attendance_logs a
USING attendance_logs b
WHERE a.member_id = b.member_id
  AND a.attendance_start = b.attendance_start
  AND a.attendance_end = b.attendance_end
  AND a.id < b.id;

DROP INDEX IF EXISTS attendance_logs_member_character_window_unique;
ALTER TABLE attendance_logs DROP CONSTRAINT IF EXISTS attendance_logs_server_member_foreign_key;
ALTER TABLE attendance_logs DROP COLUMN IF EXISTS server_member_id;
ALTER TABLE attendance_logs
    ADD CONSTRAINT attendance_logs_member_window_unique
    UNIQUE (member_id, attendance_start, attendance_end);
