package serverlog

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session resolution is a property of the stored rows, not of a query string,
// so these run against a real database. They are the regression tests for the
// six merged sessions found in production, the worst of which reported one
// visit as 33h43m.
//
// TEST_DATABASE_URL must name a disposable database: the schema is dropped.
func sessionTestPool(t *testing.T) *pgxpool.Pool {
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

// storeEvent ingests one event the way the webhook would, with a payload whose
// timestamp keeps it unique - payload is the idempotency key.
func storeEvent(t *testing.T, pool *pgxpool.Pool, status string, occurredAt time.Time, serverID int) AcceptedResult {
	t.Helper()
	payload := map[string]any{
		"event": map[string]any{"type": status, "timestamp": occurredAt.UTC().Format(time.RFC3339)},
		"player": map[string]any{
			"cid": "CID1", "name": "Kenji Nakamura", "username": "SOT - Ayvix",
			"server_id": serverID, "ping": 30,
			"identifiers": map[string]any{
				"license":  "license:31cd2c1b4837578bd4dbc7f7fec49965f29149ad",
				"discord":  "1220326041067982941",
				"steamhex": "steam:11000015eab2eb3",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRepository(pool).Store(context.Background(), ValidEvent{
		Payload:    body,
		Status:     status,
		OccurredAt: occurredAt,
		PlayerName: "Kenji Nakamura",
		Username:   "SOT - Ayvix",
		CID:        "CID1",
		License:    "license:31cd2c1b4837578bd4dbc7f7fec49965f29149ad",
		Discord:    "1220326041067982941",
		SteamHex:   "steam:11000015eab2eb3",
	})
	if err != nil {
		t.Fatalf("Store(%s) error = %v", status, err)
	}
	return result
}

func TestSessionResolution(t *testing.T) {
	pool := sessionTestPool(t)
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name string
		// each step is one event; wantSameSessionAs indexes an earlier step, or
		// -1 for "must open its own session".
		steps []struct {
			status            string
			at                time.Time
			wantSameSessionAs int
		}
	}{
		{
			name: "a normal visit is one session",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"connected", start.Add(4 * time.Minute), 0},
				{"disconnected", start.Add(5 * time.Hour), 0},
			},
		},
		{
			// The production bug. The first visit's disconnect never arrived,
			// so the session stayed open and the next visit's connected joined
			// it, reporting both visits as one.
			name: "a second connected opens a second session",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"connected", start.Add(4 * time.Minute), 0},
				{"connected", start.Add(23 * time.Hour), -1},
				{"disconnected", start.Add(25 * time.Hour), 2},
			},
		},
		{
			name: "an abandoned connecting is not joined after the grace window",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"connected", start.Add(2 * time.Hour), -1},
			},
		},
		{
			name: "a slow load inside the grace window is one session",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"connected", start.Add(25 * time.Minute), 0},
			},
		},
		{
			// Closing a day-old session would claim a day of playtime.
			name: "a disconnect past the visit bound opens its own session",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"disconnected", start.Add(13 * time.Hour), -1},
			},
		},
		{
			name: "a disconnect inside the visit bound closes the session",
			steps: []struct {
				status            string
				at                time.Time
				wantSameSessionAs int
			}{
				{"connecting", start, -1},
				{"disconnected", start.Add(9 * time.Hour), 0},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(context.Background(), "DELETE FROM server_logs; DELETE FROM server_members;"); err != nil {
				t.Fatal(err)
			}
			sessions := make([]string, 0, len(test.steps))
			for index, step := range test.steps {
				// A distinct server_id per step keeps each payload unique.
				result := storeEvent(t, pool, step.status, step.at, 100+index)
				sessions = append(sessions, result.SessionID)

				switch want := step.wantSameSessionAs; {
				case want >= 0 && result.SessionID != sessions[want]:
					t.Fatalf("step %d (%s) opened %s, want the session of step %d", index, step.status, result.SessionID, want)
				case want < 0:
					for earlier, session := range sessions[:index] {
						if session == result.SessionID {
							t.Fatalf("step %d (%s) joined step %d's session, want a new one", index, step.status, earlier)
						}
					}
				}
			}
		})
	}
}

// Resolution reads the arriving event's own timestamp, never the clock, so the
// same events in any order reach the same sessions.
func TestSessionResolutionIsOrderIndependent(t *testing.T) {
	pool := sessionTestPool(t)
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	// The connecting event overtaken by its own connected. The connected finds
	// no open session and opens one; the late connecting opens its own, which
	// splits the visit rather than losing it.
	if _, err := pool.Exec(context.Background(), "DELETE FROM server_logs; DELETE FROM server_members;"); err != nil {
		t.Fatal(err)
	}
	connected := storeEvent(t, pool, "connected", start.Add(4*time.Minute), 200)
	connecting := storeEvent(t, pool, "connecting", start, 201)
	if connecting.SessionID == connected.SessionID {
		t.Errorf("a late connecting joined the session its own connected opened")
	}

	// The disconnect still closes the connected session, since that is the
	// newest open one within the visit bound.
	disconnected := storeEvent(t, pool, "disconnected", start.Add(3*time.Hour), 200)
	if disconnected.SessionID != connected.SessionID {
		t.Errorf("disconnect opened %s, want the connected session %s", disconnected.SessionID, connected.SessionID)
	}
}

// A replay of the exact same body is deduped, so it must not disturb sessions.
func TestSessionResolutionIgnoresReplays(t *testing.T) {
	pool := sessionTestPool(t)
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(context.Background(), "DELETE FROM server_logs; DELETE FROM server_members;"); err != nil {
		t.Fatal(err)
	}

	first := storeEvent(t, pool, "connected", start, 300)
	again := storeEvent(t, pool, "connected", start, 300)
	if !again.Duplicate {
		t.Fatal("the same body was not reported as a duplicate")
	}
	if again.SessionID != first.SessionID {
		t.Errorf("replay reported %s, want %s", again.SessionID, first.SessionID)
	}

	var sessions int
	if err := pool.QueryRow(context.Background(), "SELECT count(DISTINCT session_id) FROM server_logs").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Errorf("sessions = %d, want 1", sessions)
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
