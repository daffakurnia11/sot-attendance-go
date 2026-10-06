package app

import (
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func TestConnectionStatus(t *testing.T) {
	for _, tc := range []struct {
		webhook  string
		activity presence.MemberPresence
		want     string
	}{
		{"connected", presence.MemberPresence{Status: "offline"}, "Connected"},
		{"disconnected", presence.MemberPresence{Playing: true}, "Connected"},
		{"unknown", presence.MemberPresence{Playing: true, Connecting: true}, "Connecting"},
		{"unknown", presence.MemberPresence{Status: "online"}, "Not Connected"},
		{"unknown", presence.MemberPresence{Status: "offline"}, "Not Connected"},
		{"disconnected", presence.MemberPresence{Status: "offline"}, "Not Connected"},
	} {
		if got := connectionStatus(tc.webhook, tc.activity); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}

func TestLatestWebhookStatusDoesNotPreferAbandonedSession(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
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
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE server_members (id bigint, discord_user_id text);
 CREATE TEMP TABLE server_logs (id bigint, server_member_id bigint, session_id text, status text, occurred_at timestamptz, source text);
 INSERT INTO server_members VALUES (1,'member'),(2,'member');
 INSERT INTO server_logs VALUES
 (1,1,'abandoned','connecting',NOW()-INTERVAL '2 hours','server'),
 (2,2,'latest','connected',NOW()-INTERVAL '1 hour','server'),
 (3,2,'latest','disconnected',NOW()-INTERVAL '30 minutes','server'),
 (4,1,'discord','connected',NOW(),'discord');`)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if err := tx.QueryRow(ctx, latestWebhookStatus, "member").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "disconnected" {
		t.Fatalf("got %s, want disconnected", got)
	}
}

func TestDiscordActivityLabels(t *testing.T) {
	for _, tc := range []struct {
		activity presence.MemberPresence
		want     string
	}{
		{presence.MemberPresence{}, "No Activity"},
		{presence.MemberPresence{Playing: true, Connecting: true}, "Connecting"},
		{presence.MemberPresence{Playing: true}, "Connected"},
	} {
		if got := discordActivityStatus(tc.activity); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
