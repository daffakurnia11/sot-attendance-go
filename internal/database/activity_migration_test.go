package database

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestConsolidateLegacyActivity(t *testing.T) {
	dsn := os.Getenv("ACTIVITY_MIGRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires dedicated disposable test database")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(u.Path, "test") {
		t.Fatal("requires test database name")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public; CREATE TABLE members(id bigint primary key,discord_user_id text,username text,display_name text);
 CREATE TABLE server_logs(id bigint GENERATED ALWAYS AS IDENTITY,server_member_id bigint NOT NULL,session_id uuid,status text,occurred_at timestamptz,source text,payload jsonb);
 CREATE TABLE activity_logs(id bigint primary key,member_id bigint references members(id),status text,started_at timestamptz,occurred_at timestamptz,playtime interval,created_at timestamptz);
 INSERT INTO members VALUES(1,'111','user','Name');
 INSERT INTO activity_logs VALUES(1,1,'connected','2026-08-14 21:00+07','2026-08-14 21:00+07',NULL,'2026-08-14 21:00+07'),(2,1,'disconnected','2026-08-14 21:00+07','2026-08-14 23:00+07','2 hours','2026-08-14 23:00+07');`)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.ReadFile("migrations/000054_consolidate_activity_logs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var seconds int64
	var kind string
	if err = tx.QueryRow(ctx, `SELECT relkind::text FROM pg_class WHERE oid='activity_logs'::regclass`).Scan(&kind); err != nil || kind != "v" {
		t.Fatal("physical table remains", err)
	}
	if err = tx.QueryRow(ctx, `SELECT COUNT(*),SUM(EXTRACT(EPOCH FROM playtime))::bigint FROM activity_logs`).Scan(&count, &seconds); err != nil || count != 2 || seconds != 7200 {
		t.Fatalf("history changed %d %d %v", count, seconds, err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM server_logs WHERE server_member_id IS NULL AND payload->>'discord_user_id'='111'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("identity not preserved", err)
	}
	// Old startup migrations must not recreate/index a physical activity table.
	old, _ := migrations.ReadFile("migrations/000003_create_player_logs.up.sql")
	if _, err = tx.Exec(ctx, string(old)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO activity_logs(member_id,status,occurred_at) VALUES(1,'connecting',NOW())`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM server_logs`).Scan(&count); err != nil || count != 3 {
		t.Fatal("compatibility write not stored in server_logs", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM activity_logs WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM server_logs`).Scan(&count); err != nil || count != 2 {
		t.Fatal("compatibility delete failed", err)
	}
	down, err := os.ReadFile("migrations/000054_consolidate_activity_logs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	restore := strings.ReplaceAll(strings.ReplaceAll(string(down), "BEGIN;", ""), "COMMIT;", "")
	if _, err = tx.Exec(ctx, restore); err != nil {
		t.Fatal("restore legacy table", err)
	}
	if err = tx.QueryRow(ctx, `SELECT relkind::text FROM pg_class WHERE oid='activity_logs'::regclass`).Scan(&kind); err != nil || kind != "r" {
		t.Fatal("rollback did not restore table", err)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM activity_logs`).Scan(&count); err != nil || count != 2 {
		t.Fatal("rollback lost history", err)
	}
	if _, err = tx.Exec(ctx, string(sql)); err != nil {
		t.Fatal("reapply after rollback", err)
	}

	remove, err := migrations.ReadFile("migrations/000055_drop_activity_compatibility.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = tx.Exec(ctx, string(remove)); err != nil {
			t.Fatal(err)
		}
	}
	var absent bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('public.activity_logs') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("activity relation remains", err)
	}
	if err = tx.QueryRow(ctx, "SELECT COUNT(*) FROM ("+LegacyActivitySQL+") legacy").Scan(&count); err != nil || count != 2 {
		t.Fatal("direct legacy reader lost data", err)
	}
	if _, err = tx.Exec(ctx, string(old)); err != nil {
		t.Fatal("startup recreated legacy table", err)
	}
	if err = tx.QueryRow(ctx, `SELECT to_regclass('public.activity_logs') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("old migration recreated activity relation", err)
	}

}
