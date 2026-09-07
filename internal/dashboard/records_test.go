package dashboard

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The member records total splits two feeds at a per-member handover, so it is
// arithmetic over stored rows and runs against a real database.
//
// TEST_DATABASE_URL must name a disposable database: the schema is dropped.
func recordsTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; run this against a disposable database")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if name := strings.TrimPrefix(parsed.Path, "/"); !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("refusing to drop the schema of database %q", name)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	lockTestSchema(t, databaseURL)
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return pool
}

func seedMember(t *testing.T, pool *pgxpool.Pool) (memberID int64, serverMemberID int64) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
		INSERT INTO members (discord_user_id, username, display_name)
		VALUES ('111', 'player', 'Player') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO server_members (license_id, discord_user_id, steamhex, player_name, username, cid)
		VALUES ('license:a', '111', 'steam:a', 'Player One', 'SOT - Player', 'CID1')
		RETURNING id`).Scan(&serverMemberID); err != nil {
		t.Fatal(err)
	}
	return memberID, serverMemberID
}

func seedDiscordVisit(t *testing.T, pool *pgxpool.Pool, memberID int64, startedAt, endedAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO activity_logs (member_id, status, started_at, occurred_at, playtime)
		VALUES ($1, 'disconnected', $2::timestamptz, $3::timestamptz, $3::timestamptz - $2::timestamptz)`, memberID, startedAt, endedAt); err != nil {
		t.Fatal(err)
	}
}

func seedServerVisit(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, session string, connectedAt, disconnectedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		VALUES ($1::jsonb, $2, $3, 'connected', $4)`,
		`{"seed":"`+session+`-connected"}`, serverMemberID, session, connectedAt); err != nil {
		t.Fatal(err)
	}
	if disconnectedAt.IsZero() {
		return
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		VALUES ($1::jsonb, $2, $3, 'disconnected', $4)`,
		`{"seed":"`+session+`-disconnected"}`, serverMemberID, session, disconnectedAt); err != nil {
		t.Fatal(err)
	}
}

func TestGetMemberRecordsSplitsFeedsAtTheHandover(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	memberID, serverMemberID := seedMember(t, pool)
	base := time.Now().UTC().Add(-48 * time.Hour)

	// Two hours seen only by Discord, before the webhook ever reported this
	// member, then three hours reported by the game server.
	seedDiscordVisit(t, pool, memberID, base, base.Add(2*time.Hour))
	seedServerVisit(t, pool, serverMemberID, "00000000-0000-4000-8000-000000000001", base.Add(6*time.Hour), base.Add(9*time.Hour))

	records, err := repository.GetMemberRecords(ctx, memberID)
	if err != nil {
		t.Fatalf("GetMemberRecords() error = %v", err)
	}
	if want := int64(5 * 60 * 60); records.TotalPlaytimeSeconds != want {
		t.Errorf("total = %d seconds, want %d", records.TotalPlaytimeSeconds, want)
	}

	// Discord presence that kept reporting after the handover is history, not
	// playtime: the webhook owns that period.
	seedDiscordVisit(t, pool, memberID, base.Add(6*time.Hour), base.Add(10*time.Hour))
	records, err = repository.GetMemberRecords(ctx, memberID)
	if err != nil {
		t.Fatalf("GetMemberRecords() error = %v", err)
	}
	if want := int64(5 * 60 * 60); records.TotalPlaytimeSeconds != want {
		t.Errorf("total after a post-handover Discord row = %d seconds, want %d", records.TotalPlaytimeSeconds, want)
	}
}

// A member the game server has never reported falls back to Discord entirely.
func TestGetMemberRecordsFallsBackToDiscord(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var memberID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO members (discord_user_id, username, display_name)
		VALUES ('222', 'nofivem', 'NoFiveM') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-24 * time.Hour)
	seedDiscordVisit(t, pool, memberID, base, base.Add(90*time.Minute))

	records, err := repository.GetMemberRecords(ctx, memberID)
	if err != nil {
		t.Fatalf("GetMemberRecords() error = %v", err)
	}
	if want := int64(90 * 60); records.TotalPlaytimeSeconds != want {
		t.Errorf("total = %d seconds, want %d", records.TotalPlaytimeSeconds, want)
	}
	if len(records.PlayerLogs) != 1 || records.PlayerLogs[0].Source != "discord" {
		t.Errorf("logs = %#v, want one discord row", records.PlayerLogs)
	}
}

// Overlapping webhook visits - two characters, or a session merged before
// resolution was bounded - must not count twice.
func TestGetMemberRecordsCollapsesOverlappingVisits(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	memberID, serverMemberID := seedMember(t, pool)
	var secondCharacter int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO server_members (license_id, discord_user_id, steamhex, player_name, username, cid)
		VALUES ('license:b', '111', 'steam:b', 'Player Two', 'SOT - Player', 'CID2')
		RETURNING id`).Scan(&secondCharacter); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-12 * time.Hour)
	seedServerVisit(t, pool, serverMemberID, "00000000-0000-4000-8000-000000000010", base, base.Add(2*time.Hour))
	seedServerVisit(t, pool, secondCharacter, "00000000-0000-4000-8000-000000000011", base.Add(time.Hour), base.Add(3*time.Hour))

	records, err := repository.GetMemberRecords(ctx, memberID)
	if err != nil {
		t.Fatalf("GetMemberRecords() error = %v", err)
	}
	if want := int64(3 * 60 * 60); records.TotalPlaytimeSeconds != want {
		t.Errorf("total = %d seconds, want %d (the union, not the sum)", records.TotalPlaytimeSeconds, want)
	}
}

// The list is not split: it carries both feeds, tagged.
func TestGetMemberRecordsListsBothFeeds(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	memberID, serverMemberID := seedMember(t, pool)
	base := time.Now().UTC().Add(-6 * time.Hour)
	seedDiscordVisit(t, pool, memberID, base, base.Add(time.Hour))
	seedServerVisit(t, pool, serverMemberID, "00000000-0000-4000-8000-000000000020", base.Add(2*time.Hour), base.Add(3*time.Hour))

	records, err := repository.GetMemberRecords(ctx, memberID)
	if err != nil {
		t.Fatalf("GetMemberRecords() error = %v", err)
	}
	sources := make(map[string]int)
	for _, log := range records.PlayerLogs {
		sources[log.Source]++
		if log.Source == "" {
			t.Fatalf("untagged log row: %#v", log)
		}
	}
	// One Discord disconnect, one webhook connected plus one disconnected.
	if sources["discord"] != 1 || sources["fivem"] != 2 {
		t.Errorf("sources = %#v, want 1 discord and 2 fivem", sources)
	}
	// Newest first.
	for index := 1; index < len(records.PlayerLogs); index++ {
		if records.PlayerLogs[index].OccurredAt.After(records.PlayerLogs[index-1].OccurredAt) {
			t.Fatalf("rows are not newest first: %#v", records.PlayerLogs)
		}
	}
	// The webhook disconnect reports the visit it closed.
	for _, log := range records.PlayerLogs {
		if log.Source == "fivem" && log.Status == "disconnected" {
			if log.PlaytimeSeconds == nil || *log.PlaytimeSeconds != int64(time.Hour/time.Second) {
				t.Errorf("fivem playtime = %v, want 3600", log.PlaytimeSeconds)
			}
		}
	}
}

// lockTestSchema serialises the database-backed tests across packages. Each
// resets the schema, `go test ./...` runs packages in parallel, and without
// this they drop each other's tables mid-run.
//
// The lock is held on its own connection rather than one borrowed from the
// pool: an advisory lock lives for the life of a session, so the session has to
// stay open, and a pool cannot close while a connection is checked out.
func lockTestSchema(t *testing.T, databaseURL string) {
	t.Helper()
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect for test schema lock: %v", err)
	}
	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock(hashtext('sot-test-schema'))"); err != nil {
		connection.Close(ctx)
		t.Fatalf("take test schema lock: %v", err)
	}
	t.Cleanup(func() {
		if _, err := connection.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('sot-test-schema'))"); err != nil {
			t.Logf("release test schema lock: %v", err)
		}
		connection.Close(ctx)
	})
}

// The dashboard player list follows the same rule: webhook status where it
// exists, presence where it does not, and a webhook visit whose disconnect
// never arrived must not read connected forever.
func TestDashboardPlayerStatusPrefersServerLogs(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, stubCFX{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := pool.Exec(ctx, `
		INSERT INTO settings (settings, value) VALUES ('player_threshold', '15')
		ON CONFLICT (settings) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}

	memberID, serverMemberID := seedMember(t, pool)
	now := time.Now().UTC()

	// A live visit: connected an hour ago, no disconnect yet.
	seedServerVisit(t, pool, serverMemberID, "00000000-0000-4000-8000-000000000030", now.Add(-time.Hour), time.Time{})
	// Presence disagrees and says offline; the webhook wins.
	if _, err := pool.Exec(ctx, `
		INSERT INTO activity_logs (member_id, status, started_at, occurred_at, playtime)
		VALUES ($1, 'disconnected', $2::timestamptz, $3::timestamptz, INTERVAL '30 minutes')`,
		memberID, now.Add(-3*time.Hour), now.Add(-150*time.Minute)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.Get(ctx, memberID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.DiscordPlayers) != 1 {
		t.Fatalf("players = %#v, want one", snapshot.DiscordPlayers)
	}
	player := snapshot.DiscordPlayers[0]
	if player.Status != "connected" {
		t.Errorf("status = %q, want connected from the webhook", player.Status)
	}
	if player.CurrentPlaytimeSeconds < 3500 || player.CurrentPlaytimeSeconds > 3700 {
		t.Errorf("current playtime = %d, want about 3600", player.CurrentPlaytimeSeconds)
	}

	// Age the visit past the bound: an abandoned visit reads offline.
	if _, err := pool.Exec(ctx, `
		UPDATE server_logs SET occurred_at = occurred_at - INTERVAL '13 hours'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Get(ctx, memberID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got := snapshot.DiscordPlayers[0].Status; got != "offline" {
		t.Errorf("status = %q, want offline once the visit aged out", got)
	}
}

type stubCFX struct{}

func (stubCFX) Rosters(context.Context) ([]CFXPlayer, []CFXPlayer, error) {
	return []CFXPlayer{}, []CFXPlayer{}, nil
}

// A player still on the loading screen has reported connecting and nothing
// else. Deriving status only from sessions that reached connected left them
// with no status at all, so the page never listed them.
func TestDashboardPlayerStatusShowsConnecting(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, stubCFX{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := pool.Exec(ctx, `
		INSERT INTO settings (settings, value) VALUES ('player_threshold', '15')
		ON CONFLICT (settings) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	memberID, serverMemberID := seedMember(t, pool)
	now := time.Now().UTC()

	// connecting only: no connected event, no disconnect.
	if _, err := pool.Exec(ctx, `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		VALUES ('{"seed":"arriving"}'::jsonb, $1, '00000000-0000-4000-8000-000000000040', 'connecting', $2)`,
		serverMemberID, now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.Get(ctx, memberID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(snapshot.DiscordPlayers) != 1 {
		t.Fatalf("players = %#v, want one", snapshot.DiscordPlayers)
	}
	player := snapshot.DiscordPlayers[0]
	if player.Status != "connecting" {
		t.Errorf("status = %q, want connecting", player.Status)
	}
	// Playtime starts at the connected event, which has not happened.
	if player.StartedAt != nil || player.CurrentPlaytimeSeconds != 0 {
		t.Errorf("started_at = %v, current = %d, want no playtime yet", player.StartedAt, player.CurrentPlaytimeSeconds)
	}

	// An attempt abandoned at the loading screen ages out of connecting.
	if _, err := pool.Exec(ctx, `UPDATE server_logs SET occurred_at = occurred_at - INTERVAL '31 minutes'`); err != nil {
		t.Fatal(err)
	}
	snapshot, err = repository.Get(ctx, memberID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := snapshot.DiscordPlayers[0].Status; got != "offline" {
		t.Errorf("status = %q, want offline once the attempt aged out", got)
	}
}
