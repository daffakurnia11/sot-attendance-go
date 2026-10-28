-- Preserve legacy account-level Discord events without inventing game characters.
-- The compatibility view carries no data: server_logs is the only physical store.
ALTER TABLE server_logs ALTER COLUMN server_member_id DROP NOT NULL;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='server_logs'::regclass AND conname='server_logs_identity_required') THEN
 ALTER TABLE server_logs ADD CONSTRAINT server_logs_identity_required CHECK(server_member_id IS NOT NULL OR (source='discord' AND COALESCE(payload ? 'legacy_activity',false)));
 END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS server_logs_legacy_activity_id_idx
 ON server_logs ((payload->'legacy_activity'->>'id'))
 WHERE payload ? 'legacy_activity';
CREATE INDEX IF NOT EXISTS server_logs_legacy_member_time_idx
 ON server_logs ((payload->'legacy_activity'->>'member_id'),occurred_at)
 WHERE payload ? 'legacy_activity';

DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM pg_class WHERE oid=to_regclass('public.activity_logs') AND relkind='r') THEN
  LOCK TABLE activity_logs IN ACCESS EXCLUSIVE MODE;
  INSERT INTO server_logs(server_member_id,session_id,status,occurred_at,source,payload)
  SELECT NULL,md5('legacy-activity:'||a.id::text)::uuid,a.status,a.occurred_at,'discord',
   jsonb_build_object('legacy_activity',to_jsonb(a),'discord_user_id',m.discord_user_id,
    'discord_username',m.username,'discord_display_name',m.display_name)
  FROM activity_logs a JOIN members m ON m.id=a.member_id
  ON CONFLICT ((payload->'legacy_activity'->>'id')) WHERE payload ? 'legacy_activity' DO NOTHING;
  IF EXISTS(SELECT 1 FROM activity_logs a WHERE NOT EXISTS(
   SELECT 1 FROM server_logs sl WHERE sl.payload->'legacy_activity'=to_jsonb(a))) THEN
   RAISE EXCEPTION 'Legacy activity verification failed; refusing to drop table';
  END IF;
  DROP TABLE activity_logs;
 END IF;
END $$;

CREATE OR REPLACE VIEW activity_logs AS
 SELECT (payload->'legacy_activity'->>'id')::bigint AS id,
 (payload->'legacy_activity'->>'member_id')::bigint AS member_id,
 status,
 (payload->'legacy_activity'->>'started_at')::timestamptz AS started_at,
 occurred_at,
 (payload->'legacy_activity'->>'playtime')::interval AS playtime,
 (payload->'legacy_activity'->>'created_at')::timestamptz AS created_at
 FROM server_logs WHERE payload ? 'legacy_activity';

-- Old versions/tests may still insert through the facade; every write lands in server_logs.
CREATE SEQUENCE IF NOT EXISTS legacy_activity_id_seq;
SELECT setval('legacy_activity_id_seq',GREATEST(COALESCE((SELECT max(id) FROM activity_logs),0)+1,1),false);
CREATE OR REPLACE FUNCTION write_legacy_activity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE identity_row record;
BEGIN
 IF TG_OP='DELETE' THEN
  DELETE FROM server_logs WHERE payload->'legacy_activity'->>'id'=OLD.id::text;
  RETURN OLD;
 END IF;
 SELECT discord_user_id,username,display_name INTO STRICT identity_row FROM members WHERE id=NEW.member_id;
 IF NEW.status NOT IN ('connecting','connected','disconnected') OR NEW.occurred_at IS NULL OR NEW.playtime<interval '0' THEN
  RAISE EXCEPTION 'Invalid legacy activity';
 END IF;
 NEW.id:=COALESCE(NEW.id,nextval('legacy_activity_id_seq'));
 NEW.created_at:=COALESCE(NEW.created_at,NOW());
 INSERT INTO server_logs(server_member_id,session_id,status,occurred_at,source,payload)
 VALUES(NULL,md5('legacy-activity:'||NEW.id::text)::uuid,NEW.status,NEW.occurred_at,'discord',
 jsonb_build_object('legacy_activity',to_jsonb(NEW),'discord_user_id',identity_row.discord_user_id,
 'discord_username',identity_row.username,'discord_display_name',identity_row.display_name));
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS activity_logs_compatibility_write ON activity_logs;
CREATE TRIGGER activity_logs_compatibility_write INSTEAD OF INSERT OR DELETE ON activity_logs
 FOR EACH ROW EXECUTE FUNCTION write_legacy_activity();
