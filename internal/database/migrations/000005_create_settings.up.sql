CREATE TABLE IF NOT EXISTS settings (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    settings TEXT NOT NULL,
    value TEXT NOT NULL,
    CONSTRAINT settings_name_not_blank CHECK (BTRIM(settings) <> ''),
    CONSTRAINT settings_value_not_blank CHECK (BTRIM(value) <> ''),
    CONSTRAINT settings_name_unique UNIQUE (settings)
);

INSERT INTO settings (settings, value)
VALUES
    ('start_attendance', '21:00'),
    ('end_attendance', '01:00'),
    ('playtime_threshold', '90m'),
    ('player_threshold', '15')
ON CONFLICT (settings) DO NOTHING;
