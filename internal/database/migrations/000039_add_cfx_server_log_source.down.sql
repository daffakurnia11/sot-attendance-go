DELETE FROM server_logs WHERE source = 'cfx';
ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_source_valid;
ALTER TABLE server_logs
    ADD CONSTRAINT server_logs_source_valid CHECK (source IN ('server', 'discord'));
