CREATE TABLE IF NOT EXISTS player_peak_reports (
 guild_id TEXT NOT NULL,
 report_date DATE NOT NULL,
 snapshot JSONB NOT NULL,
 member_count INTEGER NOT NULL CHECK(member_count>=0),
 captured_at TIMESTAMPTZ NOT NULL,
 delivered_message_id TEXT,
 next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(guild_id,report_date)
);
