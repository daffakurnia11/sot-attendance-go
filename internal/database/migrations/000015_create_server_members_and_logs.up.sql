-- FiveM player log webhook ingestion.
--
-- The startup runner in internal/database/database.go executes every *.up.sql
-- file on every process start and keeps no version table, so every statement
-- below must be safely re-runnable.
--
-- Two tables. server_members is one row per player identity. server_logs is one
-- row per event, append-only. Session state and playtime are derived at read
-- time, so events can arrive in any order.

CREATE TABLE IF NOT EXISTS server_members (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id BIGINT,
    license_id TEXT NOT NULL,
    discord_user_id TEXT,
    fivem_id TEXT,
    steamhex TEXT,
    player_name TEXT NOT NULL,
    username TEXT NOT NULL,
    cid TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT server_members_member_id_fkey FOREIGN KEY (member_id)
        REFERENCES members (id) ON DELETE SET NULL,
    CONSTRAINT server_members_license_id_unique UNIQUE (license_id)
);

-- An index on member_id lived here. 000024 drops that column, and the startup
-- runner re-executes every up file on every boot, so this statement failed with
-- "column member_id does not exist" the moment the column went. The column
-- itself is still declared above, inside CREATE TABLE IF NOT EXISTS, which is a
-- no-op on an existing table and gets dropped by 000024 on a fresh one.
--
-- Same rule as the note in 000017: a migration must be idempotent AND survive a
-- later migration removing what it references. Anything standing alone here has
-- to guard on the object it touches.
CREATE INDEX IF NOT EXISTS server_members_discord_user_id_idx
    ON server_members (discord_user_id);

CREATE TABLE IF NOT EXISTS server_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id TEXT NOT NULL,
    server_member_id BIGINT NOT NULL,
    session_id UUID NOT NULL,
    server_key TEXT NOT NULL,
    status TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    connected_at TIMESTAMPTZ NOT NULL,
    disconnect_reason TEXT,
    player_server_id INTEGER NOT NULL,
    player_name TEXT NOT NULL,
    ping INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT server_logs_event_id_unique UNIQUE (event_id),
    CONSTRAINT server_logs_server_member_id_fkey FOREIGN KEY (server_member_id)
        REFERENCES server_members (id) ON DELETE RESTRICT,
    CONSTRAINT server_logs_status_valid CHECK (
        status IN ('connecting', 'connected', 'disconnected')
    )
);

CREATE INDEX IF NOT EXISTS server_logs_session_occurred_at_idx
    ON server_logs (session_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS server_logs_member_occurred_at_idx
    ON server_logs (server_member_id, occurred_at DESC);
