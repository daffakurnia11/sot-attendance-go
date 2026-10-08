package serverlog

import (
	"context"
	"testing"
	"time"
)

func TestCloseForRestart(t *testing.T) {
	pool := sessionTestPool(t)
	at := time.Now().UTC().Truncate(time.Second)
	previous := at.Add(-24 * time.Hour)
	// The disconnect of the earlier session never arrived; the later visit
	// proves the player left it, so only the newest visit is still connected.
	abandoned := storeEvent(t, pool, "connected", at.Add(-13*time.Hour), 3)
	visit := storeEvent(t, pool, "connected", at.Add(-time.Hour), 1)
	repo := NewRepository(pool)
	count, err := repo.CloseForRestart(context.Background(), at, previous)
	if err != nil || count != 1 {
		t.Fatalf("close: count=%d error=%v", count, err)
	}
	count, err = repo.CloseForRestart(context.Background(), at, previous)
	if err != nil || count != 0 {
		t.Fatalf("repeat: count=%d error=%v", count, err)
	}
	var reason string
	var closed time.Time
	err = pool.QueryRow(context.Background(), `SELECT payload->'event'->>'reason', occurred_at FROM server_logs WHERE session_id=$1 AND status='disconnected'`, visit.SessionID).Scan(&reason, &closed)
	if err != nil || reason != "Scheduled restart" || !closed.Equal(at) {
		t.Fatalf("exit: %s %v %v", reason, closed, err)
	}
	var abandonedExits int
	err = pool.QueryRow(context.Background(), `SELECT count(*) FROM server_logs WHERE session_id=$1 AND status='disconnected'`, abandoned.SessionID).Scan(&abandonedExits)
	if err != nil || abandonedExits != 0 {
		t.Fatalf("abandoned visit closed: %d %v", abandonedExits, err)
	}

	storeEvent(t, pool, "connected", at.Add(time.Minute), 2)
	next := at.Add(2 * time.Hour)
	count, err = repo.CloseForRestart(context.Background(), next, at.Add(time.Hour))
	if err != nil || count != 0 {
		t.Fatalf("visit before previous restart closed: %d %v", count, err)
	}
	count, err = repo.CloseForRestart(context.Background(), next, at)
	if err != nil || count != 1 {
		t.Fatalf("visit after previous restart: count=%d error=%v", count, err)
	}
}
