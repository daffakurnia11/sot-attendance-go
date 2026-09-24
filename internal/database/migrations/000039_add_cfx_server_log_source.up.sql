-- Admits 'cfx' as a server_logs source.
--
-- The Cfx.re roster becomes a third witness to a visit, beside the CR Roleplay
-- webhook ('server') and Discord activity ('discord'). It is the only one left
-- when the webhook drops a visit and the member shows no Discord activity, and
-- without rows of its own that player was credited nothing.
--
-- Rebuilt only when the rule does not already admit 'cfx', so a replay is a
-- no-op; see 000035 for why the older migration no longer rewrites it.
DO $$
DECLARE
    existing TEXT;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO existing
    FROM pg_constraint
    WHERE conrelid = 'server_logs'::regclass AND conname = 'server_logs_source_valid';

    IF existing IS NOT NULL AND existing LIKE '%cfx%' THEN
        RETURN;
    END IF;

    ALTER TABLE server_logs DROP CONSTRAINT IF EXISTS server_logs_source_valid;
    ALTER TABLE server_logs
        ADD CONSTRAINT server_logs_source_valid CHECK (source IN ('server', 'discord', 'cfx'));
END $$;
