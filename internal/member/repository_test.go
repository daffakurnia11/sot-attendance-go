package member

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type recordingExecutor struct {
	query string
	args  []any
	err   error
	row   pgx.Row
	// queries and argsByCall keep every call, not only the last. A write that
	// reads the row back afterwards makes two, and the assertion is about the
	// first.
	queries    []string
	argsByCall [][]any
	// rows is consumed in order when set, so each call in a sequence can
	// answer with its own shape.
	rows []pgx.Row
}

func (e *recordingExecutor) record(query string, args []any) {
	e.query = query
	e.args = args
	e.queries = append(e.queries, query)
	e.argsByCall = append(e.argsByCall, args)
}

func (e *recordingExecutor) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	e.record(query, args)
	if len(e.rows) > 0 {
		next := e.rows[0]
		e.rows = e.rows[1:]
		return next
	}
	if e.row != nil {
		return e.row
	}
	return errorRow{err: e.err}
}

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

func (e *recordingExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	e.record(query, args)
	return pgconn.CommandTag{}, e.err
}

func (e *recordingExecutor) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	e.record(query, args)
	return nil, e.err
}

func TestSyncAdminsUpdatesAndClearsRolesAtomically(t *testing.T) {
	database := &recordingExecutor{}
	err := NewRepository(database).SyncAdmins(context.Background(), []string{"100", "200"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(database.query, "is_admin = (discord_user_id = ANY") || len(database.args) != 1 {
		t.Fatalf("query = %s, args = %#v", database.query, database.args)
	}
	ids, ok := database.args[0].([]string)
	if !ok || len(ids) != 2 || ids[0] != "100" || ids[1] != "200" {
		t.Fatalf("admin IDs = %#v", database.args[0])
	}
}

func TestUpsertGuildMembersBulkUpserts(t *testing.T) {
	database := &recordingExecutor{}
	err := NewRepository(database).UpsertGuildMembers(context.Background(), []Player{
		{DiscordUserID: "100", Username: "delta", DisplayName: "Delta"},
		{DiscordUserID: "200", Username: "pupaw", DisplayName: "Pupaw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(database.query, "FROM unnest") || !strings.Contains(database.query, "ON CONFLICT (discord_user_id) DO UPDATE") {
		t.Fatalf("query = %s", database.query)
	}
	// The observation time went with first_connected_at, which nothing read.
	if strings.Contains(database.query, "first_connected_at") || len(database.args) != 3 {
		t.Fatalf("query = %s, args = %#v", database.query, database.args)
	}
}

func TestFindByDiscordUserIDMapsMissingMember(t *testing.T) {
	repository := NewRepository(&recordingExecutor{err: pgx.ErrNoRows})

	_, err := repository.FindByDiscordUserID(context.Background(), "123")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByDiscordUserID() error = %v, want ErrNotFound", err)
	}
}

func TestFindByDiscordUserIDWrapsDatabaseError(t *testing.T) {
	repository := NewRepository(&recordingExecutor{err: errors.New("database unavailable")})

	_, err := repository.FindByDiscordUserID(context.Background(), "123")
	if err == nil || !strings.Contains(err.Error(), "find member by Discord user ID") {
		t.Fatalf("FindByDiscordUserID() error = %v", err)
	}
}

// The names come from server_members now, so the read has to reach them.
func TestFindByDiscordUserIDReadsNamesFromTheCharacter(t *testing.T) {
	database := &recordingExecutor{row: memberRow{member: Member{ID: 7, DiscordUserID: "123"}}}
	if _, err := NewRepository(database).FindByDiscordUserID(context.Background(), "123"); err != nil {
		t.Fatalf("FindByDiscordUserID() error = %v", err)
	}
	for _, fragment := range []string{"FROM server_members sm", "sm.discord_user_id = m.discord_user_id", "sm.player_name", "sm.username"} {
		if !strings.Contains(database.query, fragment) {
			t.Errorf("query missing %q: %s", fragment, database.query)
		}
	}
	// character_name survives only as the alias the reader returns; no dropped
	// column may be read.
	if strings.Contains(database.query, "sm.character_name") || strings.Contains(database.query, " m.character_name") || strings.Contains(database.query, "cfx_name") {
		t.Errorf("query still reads a dropped column: %s", database.query)
	}
}

type memberRow struct{ member Member }

func (r memberRow) Scan(destinations ...any) error {
	*destinations[0].(*int64) = r.member.ID
	*destinations[1].(*string) = r.member.DiscordUserID
	*destinations[2].(*string) = r.member.Username
	*destinations[3].(*string) = r.member.DisplayName
	*destinations[4].(*string) = r.member.CharacterName
	if len(destinations) > 5 {
		*destinations[5].(*string) = r.member.CFXName
	}
	if len(destinations) > 6 {
		*destinations[6].(*bool) = r.member.IsAdmin
	}
	return nil
}

func TestSaveAttendanceRecapBulkUpserts(t *testing.T) {
	database := &recordingExecutor{}
	repository := NewRepository(database)
	start := time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)

	err := repository.SaveAttendanceRecap(context.Background(), []PlaytimeRecap{
		{MemberID: 10, Playtime: 91 * time.Minute},
		{MemberID: 20, Playtime: 90 * time.Minute},
	}, start, end, 90*time.Minute)
	if err != nil {
		t.Fatalf("SaveAttendanceRecap() error = %v", err)
	}
	if !strings.Contains(database.query, "FROM unnest") || !strings.Contains(database.query, "ON CONFLICT") {
		t.Fatalf("SaveAttendanceRecap() query is not bulk upsert: %s", database.query)
	}
	attended, ok := database.args[2].([]bool)
	if !ok || len(attended) != 2 || !attended[0] || attended[1] {
		t.Fatalf("SaveAttendanceRecap() attended = %#v", database.args[2])
	}
}
