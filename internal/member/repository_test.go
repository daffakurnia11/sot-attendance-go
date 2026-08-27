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
}

func (e *recordingExecutor) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	e.query = query
	e.args = args
	if e.row != nil {
		return e.row
	}
	return errorRow{err: e.err}
}

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

func (e *recordingExecutor) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	e.query = query
	e.args = args
	return pgconn.CommandTag{}, e.err
}

func (e *recordingExecutor) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	e.query = query
	e.args = args
	return nil, e.err
}

func TestRecordLogUpsertsMemberAndInsertsLog(t *testing.T) {
	database := &recordingExecutor{}
	repository := NewRepository(database)
	connectedAt := time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC)
	playtime := 5*time.Minute + 30*time.Second

	err := repository.RecordLog(context.Background(), PlayerLog{
		Player: Player{UserID: "user", Username: "delta", DisplayName: "DeltaKilo", FirstConnectedAt: connectedAt},
		Status: "disconnected", StartedAt: &connectedAt, OccurredAt: connectedAt.Add(playtime), Playtime: &playtime,
	})
	if err != nil {
		t.Fatalf("RecordLog() error = %v", err)
	}
	if !strings.Contains(database.query, "ON CONFLICT (user_id) DO UPDATE") || !strings.Contains(database.query, "INSERT INTO player_logs") {
		t.Fatalf("RecordLog() query does not atomically save member and log: %s", database.query)
	}
	if len(database.args) != 8 || database.args[0] != "user" || database.args[4] != "disconnected" {
		t.Fatalf("RecordLog() args = %#v", database.args)
	}
	seconds, ok := database.args[7].(*int64)
	if !ok || seconds == nil || *seconds != 330 {
		t.Fatalf("RecordLog() playtime seconds = %#v", database.args[7])
	}
}

func TestRecordLogWrapsDatabaseError(t *testing.T) {
	repository := NewRepository(&recordingExecutor{err: errors.New("database unavailable")})

	err := repository.RecordLog(context.Background(), PlayerLog{})
	if err == nil || !strings.Contains(err.Error(), "record player log") {
		t.Fatalf("RecordLog() error = %v", err)
	}
}

func TestSyncAdminsUpdatesAndClearsRolesAtomically(t *testing.T) {
	database := &recordingExecutor{}
	err := NewRepository(database).SyncAdmins(context.Background(), []string{"100", "200"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(database.query, "is_admin = (user_id = ANY") || len(database.args) != 1 {
		t.Fatalf("query = %s, args = %#v", database.query, database.args)
	}
	ids, ok := database.args[0].([]string)
	if !ok || len(ids) != 2 || ids[0] != "100" || ids[1] != "200" {
		t.Fatalf("admin IDs = %#v", database.args[0])
	}
}

func TestUpsertGuildMembersBulkUpsertsWithoutReplacingFirstConnection(t *testing.T) {
	database := &recordingExecutor{}
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	err := NewRepository(database).UpsertGuildMembers(context.Background(), []Player{
		{UserID: "100", Username: "delta", DisplayName: "Delta"},
		{UserID: "200", Username: "pupaw", DisplayName: "Pupaw"},
	}, observedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(database.query, "FROM unnest") || !strings.Contains(database.query, "ON CONFLICT (user_id) DO UPDATE") || strings.Contains(database.query, "first_connected_at = EXCLUDED") {
		t.Fatalf("query = %s", database.query)
	}
	if len(database.args) != 4 || database.args[3] != observedAt {
		t.Fatalf("args = %#v", database.args)
	}
}

func TestFindByUserIDMapsMissingMember(t *testing.T) {
	repository := NewRepository(&recordingExecutor{err: pgx.ErrNoRows})

	_, err := repository.FindByUserID(context.Background(), "123")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByUserID() error = %v, want ErrNotFound", err)
	}
}

func TestFindByUserIDWrapsDatabaseError(t *testing.T) {
	repository := NewRepository(&recordingExecutor{err: errors.New("database unavailable")})

	_, err := repository.FindByUserID(context.Background(), "123")
	if err == nil || !strings.Contains(err.Error(), "find member by user ID") {
		t.Fatalf("FindByUserID() error = %v", err)
	}
}

func TestUpdateProfileIsScopedByMemberID(t *testing.T) {
	database := &recordingExecutor{row: memberRow{member: Member{ID: 7, UserID: "123", Username: "delta", DisplayName: "Delta", CharacterName: "Kenji", CFXName: "SOT - Kenji"}}}
	updated, err := NewRepository(database).UpdateProfile(context.Background(), 7, "Kenji", "SOT - Kenji")
	if err != nil {
		t.Fatalf("UpdateProfile() error = %v", err)
	}
	if updated.CharacterName != "Kenji" || updated.CFXName != "SOT - Kenji" || len(database.args) != 3 || database.args[0] != int64(7) || !strings.Contains(database.query, "WHERE id = $1") {
		t.Fatalf("update = %#v, query = %s, args = %#v", updated, database.query, database.args)
	}
}

type memberRow struct{ member Member }

func (r memberRow) Scan(destinations ...any) error {
	*destinations[0].(*int64) = r.member.ID
	*destinations[1].(*string) = r.member.UserID
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
