package app

import (
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
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

func TestStatusSessionTiming(t *testing.T) {
	now := time.Unix(1800000000, 0)
	start := now.Add(-90 * time.Minute)
	future := now.Add(time.Minute)
	for _, tc := range []struct {
		name, webhook string
		activity      presence.MemberPresence
		start         *time.Time
		want          string
	}{
		{"CR start", "connected", presence.MemberPresence{}, &start, "1h30m"},
		{"Discord fallback", "unknown", presence.MemberPresence{Playing: true, StartedAt: &start}, nil, "1h30m"},
		{"offline hides old start", "disconnected", presence.MemberPresence{}, &start, "—"},
		{"connecting has no playtime", "connecting", presence.MemberPresence{Playing: true, Connecting: true, StartedAt: &start}, nil, "—"},
		{"missing start", "connected", presence.MemberPresence{}, nil, "—"},
		{"future start", "connected", presence.MemberPresence{}, &future, "—"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connected, playtime := statusSessionTiming(tc.webhook, tc.activity, tc.start, now)
			if playtime != tc.want {
				t.Fatalf("playtime=%s want %s", playtime, tc.want)
			}
			if tc.want == "—" && connected != "—" {
				t.Fatalf("connected=%s", connected)
			}
			if tc.want != "—" && connected != "<t:1799994600:f>" {
				t.Fatalf("connected=%s", connected)
			}
		})
	}
}
