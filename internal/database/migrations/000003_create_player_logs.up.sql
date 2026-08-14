CREATE TABLE IF NOT EXISTS player_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ,
    occurred_at TIMESTAMPTZ NOT NULL,
    playtime INTERVAL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT player_logs_member_foreign_key FOREIGN KEY (member_id)
        REFERENCES members (id) ON DELETE CASCADE,
    CONSTRAINT player_logs_status_valid CHECK (
        status IN ('connecting', 'connected', 'disconnected')
    ),
    CONSTRAINT player_logs_playtime_not_negative CHECK (
        playtime IS NULL OR playtime >= INTERVAL '0 seconds'
    )
);

CREATE INDEX IF NOT EXISTS player_logs_member_id_occurred_at_idx
    ON player_logs (member_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS player_logs_occurred_at_idx
    ON player_logs (occurred_at DESC);
