-- Manual rollback only. The startup runner embeds migrations/*.up.sql and never
-- executes a .down.sql file, so nothing here runs automatically.

DROP TABLE IF EXISTS server_logs;
DROP TABLE IF EXISTS server_members;
