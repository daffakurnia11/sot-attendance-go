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

-- An index on first_connected_at lived here. 000028 drops that column, and the
-- startup runner re-executes every *.up.sql on every boot, so this statement
-- would fail with "column first_connected_at does not exist" from the moment
-- the column went - the same failure 000015's index on member_id shipped.
--
-- The column itself is still declared above, inside CREATE TABLE IF NOT EXISTS,
-- which is a no-op on an existing table and gets dropped by 000028 on a fresh
-- one. Anything standing alone here has to guard on the object it touches.
