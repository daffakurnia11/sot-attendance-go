-- Move the curated character name onto the character it names.
--
-- members.character_name sat on the Discord account, but a character name
-- belongs to a character: one account can hold several, and one already does.
-- The column also could not be reconciled with what the game reports - 6 of the
-- 21 matched players had a curated name that disagreed with their live
-- server_members.player_name beyond letter case, and in two cases named an
-- entirely different character.
--
-- The backfill therefore refuses to guess. A curated name is copied only when
-- the player holds exactly one character, so the name is unambiguously theirs,
-- or when it already matches that character's live name. Anything else is left
-- NULL and reads through to player_name, rather than stamping one character's
-- curation onto a sibling.
--
-- The UPDATE is wrapped and EXECUTEd because 000027 drops the column it reads.
-- The startup runner re-executes every *.up.sql on every boot, so a statement
-- naming members.character_name directly would fail on every boot after that
-- drop - the failure 000015 and 000017 both shipped. EXECUTE defers parsing to
-- run time, and the guard means it never runs once the column is gone.

ALTER TABLE server_members ADD COLUMN IF NOT EXISTS character_name TEXT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'members'
          AND column_name = 'character_name'
    ) THEN
        EXECUTE $backfill$
            UPDATE server_members sm
            SET character_name = NULLIF(TRIM(m.character_name), ''),
                updated_at = NOW()
            FROM members m
            WHERE sm.character_name IS NULL
              AND sm.discord_user_id IS NOT NULL
              AND m.user_id = sm.discord_user_id
              AND NULLIF(TRIM(m.character_name), '') IS NOT NULL
              AND (
                  (
                      SELECT count(*) FROM server_members peer
                      WHERE peer.discord_user_id = sm.discord_user_id
                  ) = 1
                  OR LOWER(TRIM(m.character_name)) = LOWER(TRIM(sm.player_name))
              )
        $backfill$;
    END IF;
END $$;
