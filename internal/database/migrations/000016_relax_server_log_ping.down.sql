-- Manual rollback only; the startup runner never executes a .down.sql file.
-- Will fail if any disconnected row already carries a ping.
ALTER TABLE server_logs ADD CONSTRAINT server_logs_ping_presence
    CHECK ((status = 'disconnected') = (ping IS NULL));
