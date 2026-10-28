package member

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func TestSyncRolesReconcilesExistingAccounts(t *testing.T) {
	url := os.Getenv("MEMBER_ROLE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `SET LOCAL search_path TO pg_temp;
 CREATE TEMP TABLE members(id bigint,discord_user_id text,role_id text,role_name text,is_admin boolean);
 INSERT INTO members VALUES(1,'1',NULL,NULL,true),(2,'2','old','SOT CR',false),(3,'departed','old','SOT CR',false);`)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRepository(tx)
	for range 2 {
		if err = r.SyncRoles(ctx, []Role{{DiscordUserID: "1", ID: "guest", Name: "SOT CR Guest"}, {DiscordUserID: "2"}, {DiscordUserID: "new", ID: "cr", Name: "SOT CR"}}); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT COUNT(*),bool_and(CASE WHEN id=1 THEN role_id='guest' AND role_name='SOT CR Guest' AND is_admin ELSE role_id IS NULL AND role_name IS NULL END) FROM members`).Scan(&count, &valid); err != nil || count != 3 || !valid {
		t.Fatalf("role reconciliation failed: %d %v %v", count, valid, err)
	}
	if err = r.SyncRoles(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT bool_and(role_id IS NULL AND role_name IS NULL) FROM members`).Scan(&valid); err != nil || !valid {
		t.Fatal("stale roles retained", err)
	}
}
