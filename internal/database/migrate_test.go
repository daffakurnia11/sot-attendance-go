package database

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrateAppliesTwiceOnAFreshDatabase is the check that would have caught
// two production outages.
//
// Migrate has no version table: it re-executes every *.up.sql on every process
// start. So a migration must be idempotent AND survive a later migration
// removing what it references. Both failures shipped looked identical - a
// standalone statement guarded on the object it creates rather than the object
// it touches:
//
//	000017  ADD CONSTRAINT ... CHECK (event_id ...)   after 000019 dropped event_id
//	000015  CREATE INDEX ... (member_id)              after 000024 dropped member_id
//
// Neither is visible on a first run. Both fail on the second, which is what
// every boot after the first actually does.
func TestMigrateAppliesTwiceOnAFreshDatabase(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; run this against a disposable database")
	}
	requireDisposable(t, databaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	// Start from nothing, so the run reflects a brand new deployment rather
	// than whatever a previous test left behind.
	if _, err := pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}

	// Twice, deliberately. The first pass proves the migrations build the
	// schema; the second proves they survive the runner replaying them, which
	// is what happens on every restart, deploy and crash-loop retry.
	for pass := 1; pass <= 2; pass++ {
		if err := Migrate(ctx, pool); err != nil {
			t.Fatalf("Migrate() pass %d: %v", pass, err)
		}
	}
}

// requireDisposable refuses to run against anything that does not look like a
// throwaway database, because the test drops the public schema. Pointing
// TEST_DATABASE_URL at a real database would destroy it.
func requireDisposable(t *testing.T, databaseURL string) {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("refusing to drop the schema of database %q: TEST_DATABASE_URL must name a database containing \"test\"", name)
	}
}
