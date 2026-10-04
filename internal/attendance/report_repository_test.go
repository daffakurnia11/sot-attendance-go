package attendance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServerRosterAndAttendanceWithoutAuthentication(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// Temporary tables keep this check isolated from persistent data.
	_, err = pool.Exec(ctx, `
 CREATE TEMP TABLE members (id bigint PRIMARY KEY, discord_user_id text, username text, display_name text);
 CREATE TEMP TABLE server_members (id bigint PRIMARY KEY, discord_user_id text, username text, player_name text, cid text, updated_at timestamptz);
 CREATE TEMP TABLE attendance_logs (member_id bigint NOT NULL, server_member_id bigint, attendance_start timestamptz, attendance_end timestamptz, playtime interval, required_playtime interval, is_attended boolean);
 CREATE TEMP TABLE server_logs (server_member_id bigint, occurred_at timestamptz);
 CREATE TEMP TABLE server_visits (server_member_id bigint, connected_at timestamptz, disconnected_at timestamptz, last_event_at timestamptz);
 CREATE TEMP TABLE activity_logs (member_id bigint, status text, started_at timestamptz, occurred_at timestamptz, id bigint);
 INSERT INTO members VALUES (1, '111', 'auth', 'Auth'), (2, '222', 'authonly', 'Auth only');
 INSERT INTO server_members VALUES
 (10, '111', 'game', 'Old character', 'A', '2026-09-28'),
 (11, '111', 'game', 'Latest character', 'B', '2026-10-01'),
 (12, '333', 'noauth', 'No auth', 'C', '2026-10-01');
 INSERT INTO attendance_logs VALUES
 (1, 10, '2026-09-28 21:00+07', '2026-09-29 02:00+07', interval '3 hours', interval '2 hours', true),
 (1, 11, '2026-09-28 21:00+07', '2026-09-29 02:00+07', interval '1 hour', interval '2 hours', false),
 (2, NULL, '2026-09-29 21:00+07', '2026-09-30 02:00+07', interval '3 hours', interval '2 hours', true);
 INSERT INTO server_visits VALUES (12, '2026-09-28 21:00+07', '2026-09-29 00:00+07', '2026-09-29 00:00+07');
 `)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../database/migrations/000041_decouple_attendance_from_auth.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	loc := time.FixedZone("Asia/Jakarta", 7*3600)
	start := time.Date(2026, 9, 28, 21, 0, 0, 0, loc)
	writer := member.NewRepository(pool)
	recaps, err := writer.PlaytimeRecap(ctx, start, start.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recaps) != 1 || recaps[0].MemberID != 0 || recaps[0].ServerMemberID != 12 {
		t.Fatalf("recaps = %+v", recaps)
	}
	if err = writer.SaveAttendanceRecap(ctx, recaps, start, start.Add(5*time.Hour), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	// Logging in later must not duplicate the character's previously saved night.
	if _, err = pool.Exec(ctx, `INSERT INTO members VALUES (3, '333', 'login', 'Login')`); err != nil {
		t.Fatal(err)
	}
	recaps[0].MemberID = 3
	if err = writer.SaveAttendanceRecap(ctx, recaps, start, start.Add(5*time.Hour), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM attendance_logs WHERE server_member_id=12`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("saved rows = %d, err = %v", count, err)
	}
	report, err := NewReportRepository(pool, loc).GetMonthly(ctx, 2026, time.September, 28)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Members) != 2 || report.TotalAttended != 2 || len(report.AttendanceDays) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Members[0].MemberID != 11 || report.Members[0].CharacterName != "Latest character" || report.Members[0].TotalAttended != 1 || report.Members[0].Records[0].PlaytimeSeconds != 4*3600 {
		t.Fatalf("merged account = %+v", report.Members[0])
	}
	if report.Members[1].DiscordUserID != "333" || report.Members[1].TotalAttended != 1 {
		t.Fatalf("server account = %+v", report.Members[1])
	}
}
