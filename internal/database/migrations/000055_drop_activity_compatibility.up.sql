-- All application readers now project archived history directly from server_logs.
DROP VIEW IF EXISTS activity_logs;
DROP FUNCTION IF EXISTS write_legacy_activity();
DROP SEQUENCE IF EXISTS legacy_activity_id_seq;
