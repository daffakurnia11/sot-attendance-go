-- Restore the read-only facade for older readers. Storage remains server_logs.
CREATE OR REPLACE VIEW activity_logs AS
 SELECT (payload->'legacy_activity'->>'id')::bigint AS id,
 (payload->'legacy_activity'->>'member_id')::bigint AS member_id,status,
 (payload->'legacy_activity'->>'started_at')::timestamptz AS started_at,occurred_at,
 (payload->'legacy_activity'->>'playtime')::interval AS playtime,
 (payload->'legacy_activity'->>'created_at')::timestamptz AS created_at
 FROM server_logs WHERE payload ? 'legacy_activity';
