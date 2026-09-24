package member

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// witnessCharacter seeds one member with one character and returns the
// character id.
func witnessCharacter(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "DELETE FROM server_logs; DELETE FROM server_members; DELETE FROM activity_logs; DELETE FROM members;"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO members (discord_user_id, username, display_name) VALUES ('111', 'player', 'Player')`); err != nil {
		t.Fatal(err)
	}
	var serverMemberID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO server_members (license_id, discord_user_id, steamhex, player_name, username, cid)
		VALUES ('license:c1', '111', 'steam:c1', 'MICI YU', 'sot x ywd - MICI', 'C1') RETURNING id`).Scan(&serverMemberID); err != nil {
		t.Fatal(err)
	}
	return serverMemberID
}

var witnessSessions int

// witnessVisit stores one source's visit: a connected row and, unless zero, a
// disconnected row, in a session of its own as each writer keeps them.
func witnessVisit(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, source string, connectedAt, disconnectedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	witnessSessions++
	session := fmt.Sprintf("00000000-0000-4000-8000-%012d", witnessSessions)
	rows := [][2]any{{"connected", connectedAt}}
	if !disconnectedAt.IsZero() {
		rows = append(rows, [2]any{"disconnected", disconnectedAt})
	}
	for _, row := range rows {
		payload := fmt.Sprintf(`{"source":%q,"session":%q,"event":{"type":%q,"timestamp":%q}}`, source, session, row[0], row[1].(time.Time).Format(time.RFC3339Nano))
		if _, err := pool.Exec(ctx, `
			INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
			VALUES ($1::jsonb, $2, $3, $4, $5, $6)`, payload, serverMemberID, session, row[0], row[1], source); err != nil {
			t.Fatal(err)
		}
	}
}

// Each case the three witnesses can produce, measured the way the recap
// measures it. The webhook's times win wherever it reported; the weaker two
// only add what it missed.
func TestPlaytimeRecapAcrossWitnesses(t *testing.T) {
	pool := playtimeTestPool(t)
	ctx := context.Background()
	windowStart := time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC)
	windowEnd := windowStart.Add(6 * time.Hour)
	at := func(minutes int) time.Time { return windowStart.Add(time.Duration(minutes) * time.Minute) }

	for _, test := range []struct {
		name   string
		visits []struct {
			source     string
			start, end int
		}
		want time.Duration
	}{
		{
			// Case 1. Discord saw the arrival a minute early and the exit a
			// quarter-minute late; CFX saw both minutes late. None of it
			// counts: the webhook reported both edges.
			name: "all three witnesses",
			visits: []struct {
				source     string
				start, end int
			}{{"server", 60, 180}, {"discord", 59, 181}, {"cfx", 63, 184}},
			want: 2 * time.Hour,
		},
		{
			// Case 2, activity turned off an hour in. Discord's early exit
			// does not shorten the webhook's visit.
			name: "Discord stops early",
			visits: []struct {
				source     string
				start, end int
			}{{"server", 60, 180}, {"discord", 60, 120}, {"cfx", 62, 182}},
			want: 2 * time.Hour,
		},
		{
			// Case 3: no webhook, no Discord. CFX alone is credited, which
			// before this was zero.
			name: "CFX alone",
			visits: []struct {
				source     string
				start, end int
			}{{"cfx", 65, 125}},
			want: time.Hour,
		},
		{
			// Case 4: no webhook. Discord and CFX union to the widest span
			// either saw.
			name: "Discord and CFX without the webhook",
			visits: []struct {
				source     string
				start, end int
			}{{"discord", 60, 120}, {"cfx", 63, 121}},
			want: 61 * time.Minute,
		},
		{
			// The webhook lost a whole visit later in the evening. The
			// witnesses' second visit is new time, not a lagging tail, and
			// counts on its own.
			name: "a visit only the witnesses saw",
			visits: []struct {
				source     string
				start, end int
			}{{"server", 60, 120}, {"discord", 60, 121}, {"cfx", 200, 260}},
			want: 2 * time.Hour,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			serverMemberID := witnessCharacter(t, pool)
			for _, v := range test.visits {
				witnessVisit(t, pool, serverMemberID, v.source, at(v.start), at(v.end))
			}
			recaps, err := NewRepository(pool).PlaytimeRecap(ctx, windowStart, windowEnd)
			if err != nil {
				t.Fatalf("PlaytimeRecap() error = %v", err)
			}
			if len(recaps) != 1 || recaps[0].Playtime != test.want {
				t.Fatalf("recaps = %#v, want one of %s", recaps, test.want)
			}
		})
	}
}
