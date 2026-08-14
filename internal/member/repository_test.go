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
}

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
