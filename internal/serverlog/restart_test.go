package serverlog

import (
	"context"
	"testing"
	"time"
)

func TestCloseForRestart(t *testing.T) {
	pool := sessionTestPool(t)
	at := time.Now().UTC().Truncate(time.Second)
	visit := storeEvent(t, pool, "connected", at.Add(-time.Hour), 1)
	repo := NewRepository(pool)
	count, err := repo.CloseForRestart(context.Background(), at)
	if err != nil || count != 1 {
		t.Fatalf("close: count=%d error=%v", count, err)
	}
	count, err = repo.CloseForRestart(context.Background(), at)
	if err != nil || count != 0 {
		t.Fatalf("repeat: count=%d error=%v", count, err)
	}
	var reason string
	var closed time.Time
	err = pool.QueryRow(context.Background(), `SELECT payload->'event'->>'reason', occurred_at FROM server_logs WHERE session_id=$1 AND status='disconnected'`, visit.SessionID).Scan(&reason, &closed)
	if err != nil || reason != "Scheduled restart" || !closed.Equal(at) {
		t.Fatalf("exit: %s %v %v", reason, closed, err)
	}
	storeEvent(t, pool, "connected", at.Add(time.Minute), 2)
	count, err = repo.CloseForRestart(context.Background(), at)
	if err != nil || count != 0 {
		t.Fatalf("post-restart visit closed: %d %v", count, err)
	}
}
