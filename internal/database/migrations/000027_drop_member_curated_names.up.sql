-- Drop the two curated name columns from members.
--
-- character_name moved to server_members in 000026. cfx_name has no successor:
-- it held an operator-typed copy of the CFX display name, which the webhook now
-- reports directly as server_members.username. Only 2 of 21 matched players had
-- a cfx_name that differed from their reported username, and one of those was
-- blank, so the copy was tracking the same value by hand.
--
-- Readers get both names by joining server_members on discord_user_id, which is
-- already indexed.
--
-- 000002 and 000014, which added these columns, no longer contain a statement.
-- ADD COLUMN IF NOT EXISTS is not a no-op once a column is gone, so replaying
-- them would put both columns back on every boot.

ALTER TABLE members DROP COLUMN IF EXISTS character_name;
ALTER TABLE members DROP COLUMN IF EXISTS cfx_name;
