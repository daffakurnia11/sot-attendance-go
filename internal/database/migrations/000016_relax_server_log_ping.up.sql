-- The webhook payload reports ping on every status, including disconnected.
-- Refusing it there only forced the FiveM sender to strip a field it already
-- has, so the presence rule is dropped. Range and nullability still hold.
--
-- DROP CONSTRAINT IF EXISTS is idempotent, which the startup runner requires:
-- it re-executes every *.up.sql on every boot.
ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_ping_presence;
