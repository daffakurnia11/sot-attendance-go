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

// reconciledExit stores the exit the session sweep writes when the webhook's
// never arrived.
func reconciledExit(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, session string, at time.Time) {
	t.Helper()
	payload := fmt.Sprintf(`{"session":%q,"event":{"type":"disconnected","reason":"Reconciled: absent from the CFX roster","timestamp":%q}}`, session, at.Format(time.RFC3339Nano))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		VALUES ($1::jsonb, $2, $3, 'disconnected', $4)`, payload, serverMemberID, session, at); err != nil {
		t.Fatal(err)
	}
}

// Replays two rows from the 2026-09 export. The sweep's first run, on the
// morning of 8 September, closed visits whose exits had been lost the day
// before and dated those exits to itself, so !jo's visit from 6 September
// read as covering the whole of the 7 September window. A reconciled exit
// now counts no later than twelve hours past the visit's last real event.
func TestPlaytimeRecapCapsReconciledExits(t *testing.T) {
	pool := playtimeTestPool(t)
	ctx := context.Background()
	jakarta := time.FixedZone("WIB", 7*60*60)
	at := func(day, hour, minute, second int) time.Time {
		return time.Date(2026, 9, day, hour, minute, second, 0, jakarta)
	}
	window := func(day int) (time.Time, time.Time) { return at(day, 21, 0, 0), at(day+1, 2, 0, 0) }
	recap := func(day int) time.Duration {
		t.Helper()
		start, end := window(day)
		recaps, err := NewRepository(pool).PlaytimeRecap(ctx, start, end)
		if err != nil {
			t.Fatalf("PlaytimeRecap() error = %v", err)
		}
		if len(recaps) == 0 {
			return 0
		}
		return recaps[0].Playtime
	}

	t.Run("!jo on 7 September", func(t *testing.T) {
		serverMemberID := witnessCharacter(t, pool)
		stale := "00000000-0000-4000-8000-00000000f001"
		witnessVisitSession(t, pool, serverMemberID, stale, at(6, 19, 29, 51))
		reconciledExit(t, pool, serverMemberID, stale, at(8, 9, 11, 15))
		witnessVisit(t, pool, serverMemberID, "server", at(8, 0, 17, 24), at(8, 4, 59, 3))

		// The stored figure for that night, and what the second visit alone
		// gives inside the window.
		if got, want := recap(7), time.Hour+42*time.Minute+36*time.Second; got != want {
			t.Errorf("7 September = %s, want %s", got, want)
		}
	})

	t.Run("KANG PRI on 6 and 7 September", func(t *testing.T) {
		serverMemberID := witnessCharacter(t, pool)
		stale := "00000000-0000-4000-8000-00000000f002"
		witnessVisitSession(t, pool, serverMemberID, stale, at(6, 23, 52, 25))
		reconciledExit(t, pool, serverMemberID, stale, at(8, 8, 17, 40))

		// The lost exit still counts to the end of the night it began in;
		// it no longer reaches the next night.
		if got, want := recap(6), 2*time.Hour+7*time.Minute+35*time.Second; got != want {
			t.Errorf("6 September = %s, want %s", got, want)
		}
		if got := recap(7); got != 0 {
			t.Errorf("7 September = %s, want 0: the visit's last real event was a day earlier", got)
		}
	})
}

// witnessVisitSession stores a webhook connected row in a named session.
func witnessVisitSession(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, session string, connectedAt time.Time) {
	t.Helper()
	payload := fmt.Sprintf(`{"session":%q,"event":{"type":"connected","timestamp":%q}}`, session, connectedAt.Format(time.RFC3339Nano))
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
		VALUES ($1::jsonb, $2, $3, 'connected', $4)`, payload, serverMemberID, session, connectedAt); err != nil {
		t.Fatal(err)
	}
}
