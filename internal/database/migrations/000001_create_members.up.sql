CREATE TABLE IF NOT EXISTS members (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    display_name TEXT NOT NULL,
    first_connected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT members_user_id_not_blank CHECK (user_id <> ''),
    CONSTRAINT members_user_id_unique UNIQUE (user_id)
);

CREATE INDEX IF NOT EXISTS members_first_connected_at_idx ON members (first_connected_at);
