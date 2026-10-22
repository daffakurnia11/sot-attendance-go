package app

import (
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/playerreport"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

func TestPlayerPeakPersistence(t *testing.T) {
	url := os.Getenv("PLAYER_REPORT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE player_peak_reports(guild_id text,report_date date,snapshot jsonb,member_count int,captured_at timestamptz,delivered_message_id text,posted_count int NOT NULL DEFAULT -1,next_attempt_at timestamptz DEFAULT NOW(),PRIMARY KEY(guild_id,report_date)); SET LOCAL search_path TO pg_temp;`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	for index, count := range []int{2, 1, 2, 4, 4} {
		snapshot := playerreport.Snapshot{At: at.Add(time.Duration(index) * time.Minute), Players: make([]playerreport.Player, count)}
		if err = savePlayerPeak(ctx, tx, "guild", "2026-10-22", snapshot); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var captured time.Time
	if err = tx.QueryRow(ctx, `SELECT member_count,captured_at FROM player_peak_reports`).Scan(&count, &captured); err != nil {
		t.Fatal(err)
	}
	if count != 4 || !captured.Equal(at.Add(3*time.Minute)) {
		t.Fatal("peak or earliest tie was lost")
	}
	_, err = tx.Exec(ctx, `UPDATE player_peak_reports SET delivered_message_id='sent'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = savePlayerPeak(ctx, tx, "guild", "2026-10-22", playerreport.Snapshot{At: at, Players: make([]playerreport.Player, 5)}); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `SELECT member_count FROM player_peak_reports`).Scan(&count); err != nil || count != 5 {
		t.Fatal("higher peak did not replace posted peak")
	}
}
