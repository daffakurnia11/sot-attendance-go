package serverlog

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

const kenjiDiscord = "1220326041067982941"

// insertWitnessRow stores one row as a witness writer would.
func insertWitnessRow(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, source, session, status string, occurredAt time.Time) {
	t.Helper()
	payload := fmt.Sprintf(`{"source":%q,"session":%q,"event":{"type":%q,"timestamp":%q}}`, source, session, status, occurredAt.UTC().Format(time.RFC3339Nano))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
		VALUES ($1::jsonb, $2, $3, $4, $5, $6)`, payload, serverMemberID, session, status, occurredAt, source); err != nil {
		t.Fatal(err)
	}
}

// A webhook event joins the webhook's own open visit, never a witness's. A
// Discord connecting row between the webhook's connecting and connected used
// to capture the connected and the exit, leaving the webhook's visit open
// with no disconnect for the sweep or the twelve hour cap to end.
func TestWebhookEventsIgnoreWitnessSessions(t *testing.T) {
	pool := sessionTestPool(t)
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	connecting := storeEvent(t, pool, StatusConnecting, start, 900)
	insertWitnessRow(t, pool, connecting.ServerMemberID, SourceDiscord, "00000000-0000-4000-8000-00000000d001", StatusConnecting, start.Add(5*time.Second))

	connected := storeEvent(t, pool, StatusConnected, start.Add(time.Minute), 12)
	disconnected := storeEvent(t, pool, StatusDisconnected, start.Add(30*time.Minute), 12)
	if connected.SessionID != connecting.SessionID || disconnected.SessionID != connecting.SessionID {
		t.Fatalf("sessions = %s, %s, %s; want all on the webhook's %s", connecting.SessionID, connected.SessionID, disconnected.SessionID, connecting.SessionID)
	}
}

// Case 3: the roster is the only witness. Names resolve to characters the way
// the sweep matches them, unknown names are skipped, a repeat writes nothing,
// and a close can never predate its open.
func TestRecordCFXPresence(t *testing.T) {
	pool := sessionTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool)
	opened := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	storeVisit(t, pool, "SOT - Paw", "PAW1", "222", opened.Add(-24*time.Hour), 5)

	opens := []CFXSighting{{Username: " sot - PAW ", ServerID: 42, At: opened}, {Username: "Stranger", At: opened}}
	written, err := repository.RecordCFXPresence(ctx, opens, nil)
	if err != nil || written != 1 {
		t.Fatalf("RecordCFXPresence(open) = %d, %v; want 1 row for the known name only", written, err)
	}
	if written, err := repository.RecordCFXPresence(ctx, opens, nil); err != nil || written != 0 {
		t.Fatalf("repeat open = %d, %v; want nothing", written, err)
	}
	open, err := repository.OpenCFXVisits(ctx)
	if _, found := open["sot - paw"]; err != nil || !found || len(open) != 1 {
		t.Fatalf("OpenCFXVisits() = %v, %v; want only sot - paw", open, err)
	}

	var slot string
	if err := pool.QueryRow(ctx, `SELECT payload->'player'->>'server_id' FROM server_logs WHERE source = 'cfx'`).Scan(&slot); err != nil || slot != "42" {
		t.Fatalf("stored slot = %q, %v; want 42", slot, err)
	}

	closes := []CFXSighting{{Username: "SOT - Paw", At: opened.Add(-time.Hour)}}
	if written, err := repository.RecordCFXPresence(ctx, nil, closes); err != nil || written != 1 {
		t.Fatalf("RecordCFXPresence(close) = %d, %v; want 1", written, err)
	}
	var ended time.Time
	if err := pool.QueryRow(ctx, `SELECT occurred_at FROM server_logs WHERE source = 'cfx' AND status = 'disconnected'`).Scan(&ended); err != nil {
		t.Fatal(err)
	}
	if !ended.Equal(opened) {
		t.Errorf("close dated %s, want clamped to the open at %s", ended, opened)
	}
	if open, _ := repository.OpenCFXVisits(ctx); len(open) != 0 {
		t.Errorf("OpenCFXVisits() after close = %v, want none", open)
	}

	// Every boot replays every migration. With 'cfx' rows stored, a replay
	// that narrowed the source rule back to two values would abort startup.
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migration replay with CFX rows stored: %v", err)
	}
}

// Case 4: Discord activity is credited to the character the game is showing,
// and a visit it opened is closed on that character even after the pick moves.
func TestDiscordPresenceFollowsThePlayedCharacter(t *testing.T) {
	pool := sessionTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool)
	now := time.Now().UTC()

	// The played character, with an open webhook visit, and a newer one the
	// member is not on.
	storeVisit(t, pool, "SOT - Old", "OLD1", "333", now.Add(-30*time.Minute), 7)
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_members (license_id, discord_user_id, steamhex, player_name, username, cid, updated_at)
		VALUES ('license:NEW1', '333', 'steam:new1', 'New', 'SOT - New', 'NEW1', NOW() + INTERVAL '1 minute')`); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.RecordDiscordPresence(ctx, []DiscordPresence{{DiscordUserID: "333", Playing: true, StartedAt: now.Add(-20 * time.Minute)}}, "CR Roleplay"); err != nil {
		t.Fatal(err)
	}
	var credited string
	if err := pool.QueryRow(ctx, `
		SELECT sm.cid FROM server_logs sl JOIN server_members sm ON sm.id = sl.server_member_id
		WHERE sl.source = 'discord' AND sl.status = 'connected'`).Scan(&credited); err != nil {
		t.Fatal(err)
	}
	if credited != "OLD1" {
		t.Fatalf("Discord visit credited to %s, want OLD1, the character on the server", credited)
	}

	// The webhook visit ends, so the pick would now fall to the newer
	// character; the Discord visit still closes where it opened.
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		SELECT '{"close":1}'::jsonb, server_member_id, session_id, 'disconnected', NOW()
		FROM server_logs WHERE source = 'server' LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecordDiscordPresence(ctx, []DiscordPresence{{DiscordUserID: "333", Playing: false}}, "CR Roleplay"); err != nil {
		t.Fatal(err)
	}
	var closedOn string
	if err := pool.QueryRow(ctx, `
		SELECT sm.cid FROM server_logs sl JOIN server_members sm ON sm.id = sl.server_member_id
		WHERE sl.source = 'discord' AND sl.status = 'disconnected'`).Scan(&closedOn); err != nil {
		t.Fatalf("no Discord disconnect written: %v", err)
	}
	if closedOn != "OLD1" {
		t.Errorf("Discord visit closed on %s, want OLD1 where it opened", closedOn)
	}
}

// The announcer drops a weaker witness's exit only while a more trusted one
// still has the player on the server at that moment.
func TestAnnouncementsCarryTrustedOpenSince(t *testing.T) {
	pool := sessionTestPool(t)
	ctx := context.Background()
	start := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)

	visit := storeEvent(t, pool, StatusConnected, start, 12)
	insertWitnessRow(t, pool, visit.ServerMemberID, SourceDiscord, "00000000-0000-4000-8000-00000000d101", StatusDisconnected, start.Add(time.Hour))
	storeEvent(t, pool, StatusDisconnected, start.Add(2*time.Hour), 12)
	insertWitnessRow(t, pool, visit.ServerMemberID, SourceDiscord, "00000000-0000-4000-8000-00000000d102", StatusDisconnected, start.Add(2*time.Hour+15*time.Second))

	announcements, err := NewRepository(pool).AnnouncementsAfter(ctx, 0, 10)
	if err != nil || len(announcements) != 4 {
		t.Fatalf("AnnouncementsAfter() = %d rows, %v; want 4", len(announcements), err)
	}
	for _, a := range announcements {
		if a.ServerMemberID != visit.ServerMemberID {
			t.Errorf("row %d character = %d, want %d", a.ID, a.ServerMemberID, visit.ServerMemberID)
		}
	}
	if announcements[0].TrustedOpenSince != nil || announcements[2].TrustedOpenSince != nil {
		t.Errorf("webhook rows carry a trusted open visit; nothing outranks the webhook")
	}
	if since := announcements[1].TrustedOpenSince; since == nil || !since.Equal(start) {
		t.Errorf("mid-visit Discord exit: trusted open since = %v, want %s", since, start)
	}
	if since := announcements[3].TrustedOpenSince; since != nil {
		t.Errorf("Discord exit after the webhook's: trusted open since = %v, want none", since)
	}
}

// A lost exit is dated to the last time any source saw the player, not to the
// sweep. The first sweep ever run dated exits a day late, and those visits
// then counted a whole extra day of playtime.
func TestCloseIdleSessionsDatesTheExitAtTheLastSighting(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	connected := now.Add(-3 * time.Hour)
	cfxLastSeen := now.Add(-2 * time.Hour)
	discordLastSeen := now.Add(-100 * time.Minute)

	for _, test := range []struct {
		name     string
		lastSeen func(username string) LastSightings
		want     time.Time
	}{
		{
			name: "the later of the CFX and Discord sightings",
			lastSeen: func(username string) LastSightings {
				return LastSightings{
					Names:   map[string]time.Time{username: cfxLastSeen},
					Discord: map[string]time.Time{kenjiDiscord: discordLastSeen},
				}
			},
			want: discordLastSeen,
		},
		{
			name:     "no sighting falls back to the last webhook event",
			lastSeen: func(string) LastSightings { return LastSightings{} },
			want:     connected,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := sessionTestPool(t)
			storeEvent(t, pool, StatusConnected, connected, 400)
			var username string
			if err := pool.QueryRow(ctx, "SELECT username FROM server_members LIMIT 1").Scan(&username); err != nil {
				t.Fatal(err)
			}
			closed, err := NewRepository(pool).CloseIdleSessions(ctx, []string{"someone else"}, everSeen(t, pool), nil, now, ReconcileGrace, test.lastSeen(username))
			if err != nil || closed != 1 {
				t.Fatalf("CloseIdleSessions() = %d, %v; want 1", closed, err)
			}
			var exit time.Time
			if err := pool.QueryRow(ctx, `SELECT occurred_at FROM server_logs WHERE status = 'disconnected'`).Scan(&exit); err != nil {
				t.Fatal(err)
			}
			if !exit.Equal(test.want) {
				t.Errorf("exit dated %s, want %s", exit, test.want)
			}
		})
	}
}
