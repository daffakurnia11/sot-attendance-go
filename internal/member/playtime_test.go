package member

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PlaytimeRecap is arithmetic over stored rows, so these run against a real
// database rather than asserting on a query string.
//
// TEST_DATABASE_URL must name a disposable database: the schema is dropped.
func playtimeTestPool(t *testing.T) *pgxpool.Pool {
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

// visit is one session's worth of rows to seed. A zero disconnectedAt leaves the
// visit open.
type visit struct {
	character      string
	connectedAt    time.Time
	disconnectedAt time.Time
}

func seedVisits(t *testing.T, pool *pgxpool.Pool, discordUserID string, visits []visit) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO members (discord_user_id, username, display_name)
		VALUES ($1, 'player', 'Player')
		ON CONFLICT (discord_user_id) DO NOTHING`, discordUserID); err != nil {
		t.Fatal(err)
	}

	for index, v := range visits {
		var serverMemberID int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO server_members (license_id, discord_user_id, steamhex, player_name, username, cid)
			VALUES ($1, $2, $3, $4, 'SOT - Player', $5)
			ON CONFLICT (cid, steamhex) DO UPDATE SET updated_at = NOW()
			RETURNING id`,
			"license:"+v.character, discordUserID, "steam:"+v.character, v.character, v.character,
		).Scan(&serverMemberID); err != nil {
			t.Fatal(err)
		}

		sessionID := fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		if _, err := pool.Exec(ctx, `
			INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
			VALUES ($1::jsonb, $2, $3, 'connected', $4)`,
			fmt.Sprintf(`{"seed":"%s-connected-%d"}`, discordUserID, index), serverMemberID, sessionID, v.connectedAt,
		); err != nil {
			t.Fatal(err)
		}
		if v.disconnectedAt.IsZero() {
			continue
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
			VALUES ($1::jsonb, $2, $3, 'disconnected', $4)`,
			fmt.Sprintf(`{"seed":"%s-disconnected-%d"}`, discordUserID, index), serverMemberID, sessionID, v.disconnectedAt,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlaytimeRecapFromServerLogs(t *testing.T) {
	pool := playtimeTestPool(t)
	// The real attendance window: 21:00 to 02:00 Asia/Jakarta, five hours.
	windowStart := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)

	for _, test := range []struct {
		name   string
		visits []visit
		want   time.Duration
	}{
		{
			name:   "one visit inside the window",
			visits: []visit{{"CHAR1", windowStart.Add(30 * time.Minute), windowStart.Add(150 * time.Minute)}},
			want:   120 * time.Minute,
		},
		{
			name: "two separate visits add up",
			visits: []visit{
				{"CHAR1", windowStart, windowStart.Add(time.Hour)},
				{"CHAR1", windowStart.Add(2 * time.Hour), windowStart.Add(3 * time.Hour)},
			},
			want: 2 * time.Hour,
		},
		{
			// Overlapping visits on the same character still count once: that
			// is a session merged before resolution was bounded, not two
			// places at once.
			name: "overlapping visits on one character count once",
			visits: []visit{
				{"CHAR1", windowStart, windowEnd},
				{"CHAR1", windowStart.Add(time.Hour), windowStart.Add(2 * time.Hour)},
			},
			want: 5 * time.Hour,
		},
		{
			name: "partly overlapping visits on one character merge into their union",
			visits: []visit{
				{"CHAR1", windowStart, windowStart.Add(2 * time.Hour)},
				{"CHAR1", windowStart.Add(time.Hour), windowStart.Add(3 * time.Hour)},
			},
			want: 3 * time.Hour,
		},
		{
			name:   "a visit straddling the window is clamped to it",
			visits: []visit{{"CHAR1", windowStart.Add(-2 * time.Hour), windowEnd.Add(2 * time.Hour)}},
			want:   5 * time.Hour,
		},
		{
			name:   "a visit still open at the window end counts to the end",
			visits: []visit{{"CHAR1", windowStart.Add(time.Hour), time.Time{}}},
			want:   4 * time.Hour,
		},
		{
			// Its last event is older than visitMaxAge before the window, so it
			// is an abandoned visit rather than a player still on.
			name:   "an abandoned visit ages out",
			visits: []visit{{"CHAR1", windowStart.Add(-13 * time.Hour), time.Time{}}},
			want:   0,
		},
		{
			name:   "a visit entirely before the window counts nothing",
			visits: []visit{{"CHAR1", windowStart.Add(-3 * time.Hour), windowStart.Add(-2 * time.Hour)}},
			want:   0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM members;"); err != nil {
				t.Fatal(err)
			}
			seedVisits(t, pool, "111", test.visits)

			recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
			if err != nil {
				t.Fatalf("PlaytimeRecap() error = %v", err)
			}
			if test.want == 0 {
				if len(recaps) != 0 {
					t.Fatalf("recaps = %#v, want none", recaps)
				}
				return
			}
			if len(recaps) != 1 {
				t.Fatalf("recaps = %#v, want one", recaps)
			}
			if recaps[0].Playtime != test.want {
				t.Errorf("playtime = %s, want %s", recaps[0].Playtime, test.want)
			}
		})
	}
}

// Presence is the fallback where the game server reported nothing: a member it
// has never seen is measured entirely from activity_logs.
func TestPlaytimeRecapFallsBackToPresence(t *testing.T) {
	pool := playtimeTestPool(t)
	windowStart := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM members;"); err != nil {
		t.Fatal(err)
	}
	seedVisits(t, pool, "111", nil)

	var memberID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM members WHERE discord_user_id = '111'").Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO activity_logs (member_id, status, started_at, occurred_at, playtime)
		VALUES ($1, 'disconnected', $2, $3, INTERVAL '3 hours')`,
		memberID, windowStart, windowStart.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("PlaytimeRecap() error = %v", err)
	}
	if len(recaps) != 1 {
		t.Fatalf("recaps = %#v, want one from the presence fallback", recaps)
	}
	if recaps[0].Playtime != 3*time.Hour {
		t.Errorf("playtime = %s, want 3h from activity_logs", recaps[0].Playtime)
	}
}

// Presence that kept reporting after the game server took over is history, not
// playtime: the webhook owns every period from the handover onward.
func TestPlaytimeRecapPrefersServerLogsAfterTheHandover(t *testing.T) {
	pool := playtimeTestPool(t)
	windowStart := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM activity_logs; DELETE FROM members;"); err != nil {
		t.Fatal(err)
	}
	// One webhook visit of two hours inside the window, and a presence row
	// claiming four hours over the same period.
	seedVisits(t, pool, "111", []visit{{"CHAR1", windowStart, windowStart.Add(2 * time.Hour)}})

	var memberID int64
	if err := pool.QueryRow(ctx, "SELECT id FROM members WHERE discord_user_id = '111'").Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO activity_logs (member_id, status, started_at, occurred_at, playtime)
		VALUES ($1, 'disconnected', $2::timestamptz, $3::timestamptz, INTERVAL '4 hours')`,
		memberID, windowStart, windowStart.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}

	recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("PlaytimeRecap() error = %v", err)
	}
	if len(recaps) != 1 {
		t.Fatalf("recaps = %#v, want one", recaps)
	}
	if recaps[0].Playtime != 2*time.Hour {
		t.Errorf("playtime = %s, want 2h: the webhook owns the period, presence must not add to it", recaps[0].Playtime)
	}
}

// lockTestSchema serialises the database-backed tests across packages. Each
// resets the schema, `go test ./...` runs packages in parallel, and without
// this they drop each other's tables mid-run.
//
// The lock is held on its own connection rather than one borrowed from the
// pool: an advisory lock lives for the life of a session, so the session has to
// stay open, and a pool cannot close while a connection is checked out - the
// first version of this deadlocked against pool.Close.
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

// Two characters under one Discord account are two rows, each carrying its own
// playtime. Valencia Wang is the case this was built from: 138 minutes on one
// character and 115 on the other, back to back.
func TestPlaytimeRecapRowPerCharacter(t *testing.T) {
	pool := playtimeTestPool(t)
	windowStart := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM activity_logs; DELETE FROM members;"); err != nil {
		t.Fatal(err)
	}
	seedVisits(t, pool, "111", []visit{
		{"VWANG", windowStart.Add(41 * time.Minute), windowStart.Add(179 * time.Minute)},
		{"HYUNA", windowStart.Add(184 * time.Minute), windowStart.Add(299 * time.Minute)},
	})

	recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("PlaytimeRecap() error = %v", err)
	}
	if len(recaps) != 2 {
		t.Fatalf("recaps = %#v, want one row per character", recaps)
	}
	got := map[string]time.Duration{}
	for _, recap := range recaps {
		got[recap.CharacterName] = recap.Playtime
		if recap.ServerMemberID == 0 || recap.CID == "" {
			t.Errorf("row %q has no character: %#v", recap.CharacterName, recap)
		}
		if recap.DiscordUserID != "111" {
			t.Errorf("row %q lost the shared Discord account: %#v", recap.CharacterName, recap)
		}
	}
	if got["VWANG"] != 138*time.Minute || got["HYUNA"] != 115*time.Minute {
		t.Errorf("split = %#v, want VWANG 138m and HYUNA 115m", got)
	}
}

// A member the game server has never reported has no character, so their
// presence fallback row carries none.
func TestPlaytimeRecapPresenceRowHasNoCharacter(t *testing.T) {
	pool := playtimeTestPool(t)
	windowStart := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(5 * time.Hour)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM activity_logs; DELETE FROM members;"); err != nil {
		t.Fatal(err)
	}
	var memberID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO members (discord_user_id, username, display_name)
		VALUES ('222', 'nofivem', 'NoFiveM') RETURNING id`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO activity_logs (member_id, status, started_at, occurred_at, playtime)
		VALUES ($1, 'disconnected', $2::timestamptz, $3::timestamptz, INTERVAL '3 hours')`,
		memberID, windowStart, windowStart.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
	if err != nil {
		t.Fatalf("PlaytimeRecap() error = %v", err)
	}
	if len(recaps) != 1 || recaps[0].ServerMemberID != 0 || recaps[0].Playtime != 3*time.Hour {
		t.Fatalf("recaps = %#v, want one character-less row of 3h", recaps)
	}
}
