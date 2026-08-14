CREATE TABLE IF NOT EXISTS attendance_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    member_id BIGINT NOT NULL,
    attendance_start TIMESTAMPTZ NOT NULL,
    attendance_end TIMESTAMPTZ NOT NULL,
    playtime INTERVAL NOT NULL,
    required_playtime INTERVAL NOT NULL,
    is_attended BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT attendance_logs_member_foreign_key FOREIGN KEY (member_id)
        REFERENCES members (id) ON DELETE CASCADE,
    CONSTRAINT attendance_logs_window_valid CHECK (
        attendance_end > attendance_start
    ),
    CONSTRAINT attendance_logs_playtime_not_negative CHECK (
        playtime >= INTERVAL '0 seconds'
    ),
    CONSTRAINT attendance_logs_required_playtime_positive CHECK (
        required_playtime > INTERVAL '0 seconds'
    ),
    CONSTRAINT attendance_logs_member_window_unique UNIQUE (
        member_id, attendance_start, attendance_end
    )
);

CREATE INDEX IF NOT EXISTS attendance_logs_window_idx
    ON attendance_logs (attendance_start DESC, attendance_end DESC);

CREATE INDEX IF NOT EXISTS attendance_logs_member_id_idx
    ON attendance_logs (member_id, attendance_start DESC);
