package database

// LegacyActivitySQL projects preserved account-level history from the sole event
// store. Original IDs/timing remain intact; no activity_logs relation is required.
const LegacyActivitySQL = `SELECT
 (payload->'legacy_activity'->>'id')::bigint AS id,
 (payload->'legacy_activity'->>'member_id')::bigint AS member_id,
 status,(payload->'legacy_activity'->>'started_at')::timestamptz AS started_at,
 occurred_at,(payload->'legacy_activity'->>'playtime')::interval AS playtime,
 (payload->'legacy_activity'->>'created_at')::timestamptz AS created_at
 FROM server_logs WHERE payload ? 'legacy_activity'`
