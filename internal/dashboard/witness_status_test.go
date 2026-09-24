package dashboard

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedWitnessRow stores one discord or cfx row, with an optional slot.
func seedWitnessRow(t *testing.T, pool *pgxpool.Pool, serverMemberID int64, source, session, status string, occurredAt time.Time, slot int) {
	t.Helper()
	payload := fmt.Sprintf(`{"seed":%q,"status":%q}`, session, status)
	if slot > 0 {
		payload = fmt.Sprintf(`{"seed":%q,"status":%q,"player":{"server_id":%d}}`, session, status, slot)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
		VALUES ($1::jsonb, $2, $3, $4, $5, $6)`, payload, serverMemberID, session, status, occurredAt, source); err != nil {
		t.Fatal(err)
	}
}

// The dashboard's status follows the most trusted live witness, and a weaker
// witness's lagging tail cannot hold a player online after the webhook's exit.
func TestDashboardPlayerStatusAcrossWitnesses(t *testing.T) {
	pool := recordsTestPool(t)
	ctx := context.Background()
	repository := NewRepository(pool, stubCFX{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := pool.Exec(ctx, `
		INSERT INTO settings (settings, value) VALUES ('player_threshold', '15')
		ON CONFLICT (settings) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatal(err)
	}
	memberID, serverMemberID := seedMember(t, pool)
	now := time.Now().UTC()
	ago := func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	const (
		serverSession  = "00000000-0000-4000-8000-0000000000a1"
		discordSession = "00000000-0000-4000-8000-0000000000d1"
		cfxSession     = "00000000-0000-4000-8000-0000000000c1"
	)

	for _, test := range []struct {
		name       string
		seed       func()
		wantStatus string
		wantSlot   string
	}{
		{
			// Case 2: activity off mid-visit. The webhook still has them.
			name: "Discord closed while the webhook is open",
			seed: func() {
				seedServerVisit(t, pool, serverMemberID, serverSession, ago(60), time.Time{})
				seedWitnessRow(t, pool, serverMemberID, "discord", discordSession, "connected", ago(60), 0)
				seedWitnessRow(t, pool, serverMemberID, "discord", discordSession, "disconnected", ago(30), 0)
			},
			wantStatus: "connected",
		},
		{
			// The webhook reported the exit; Discord has not caught up.
			name: "a lagging witness after the webhook's exit",
			seed: func() {
				seedServerVisit(t, pool, serverMemberID, serverSession, ago(60), ago(1))
				seedWitnessRow(t, pool, serverMemberID, "discord", discordSession, "connected", ago(60), 0)
			},
			wantStatus: "offline",
		},
		{
			// Case 3: the roster alone, with its slot.
			name: "CFX alone",
			seed: func() {
				seedWitnessRow(t, pool, serverMemberID, "cfx", cfxSession, "connected", ago(30), 42)
			},
			wantStatus: "connected",
			wantSlot:   "42",
		},
		{
			// Case 4 after a webhook exit: a visit that opened later is one
			// the webhook missed, not a tail.
			name: "a witness visit opened after the webhook's exit",
			seed: func() {
				seedServerVisit(t, pool, serverMemberID, serverSession, ago(180), ago(120))
				seedWitnessRow(t, pool, serverMemberID, "discord", discordSession, "connected", ago(20), 0)
			},
			wantStatus: "connected",
		},
		{
			// Image 1: Discord saw the arrival before the webhook did.
			name: "a witness's arrival beats the webhook's connecting",
			seed: func() {
				if _, err := pool.Exec(ctx, `
					INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
					VALUES ('{"seed":"connecting"}'::jsonb, $1, $2, 'connecting', $3)`, serverMemberID, serverSession, ago(2)); err != nil {
					t.Fatal(err)
				}
				seedWitnessRow(t, pool, serverMemberID, "discord", discordSession, "connected", ago(1), 0)
			},
			wantStatus: "connected",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, "DELETE FROM server_logs"); err != nil {
				t.Fatal(err)
			}
			test.seed()
			snapshot, err := repository.Get(ctx, memberID)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if len(snapshot.DiscordPlayers) != 1 {
				t.Fatalf("players = %#v, want one", snapshot.DiscordPlayers)
			}
			player := snapshot.DiscordPlayers[0]
			if player.Status != test.wantStatus {
				t.Errorf("status = %q, want %q", player.Status, test.wantStatus)
			}
			slot := ""
			if player.ServerID != nil {
				slot = *player.ServerID
			}
			if test.wantSlot != "" && slot != test.wantSlot {
				t.Errorf("slot = %q, want %q", slot, test.wantSlot)
			}
		})
	}
}
