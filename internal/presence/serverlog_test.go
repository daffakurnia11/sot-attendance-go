package presence

import (
	"testing"
	"time"
)

func TestServerLogEmbedFooterMarksDirectServer(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	embed := ServerLogEmbed(ServerLogEvent{
		PlayerName: "SOT - Ayvix",
		Username:   "Kenji Nakamura",
		Status:     "connected",
		OccurredAt: start,
		StartedAt:  start,
	}, 3)

	if embed.Title != "SOT - Ayvix (Kenji Nakamura)" {
		t.Fatalf("Title = %q", embed.Title)
	}
	if embed.Footer == nil || embed.Footer.Text != "SOT Players: 3 • Direct Server" {
		t.Fatalf("Footer = %#v", embed.Footer)
	}
	if embed.Timestamp != start.Format(time.RFC3339) {
		t.Fatalf("Timestamp = %q", embed.Timestamp)
	}
}

// The two feeds must be distinguishable by footer alone, since they share a
// channel.
func TestServerLogEmbedFooterDiffersFromDiscordActivity(t *testing.T) {
	if sourceDirectServer == sourceDiscordActivity {
		t.Fatal("both feeds carry the same source label")
	}
}

func TestServerLogEmbedPerStatus(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)

	for _, test := range []struct {
		status     string
		wantStatus string
		wantColor  int
		wantFields []string
	}{
		{"connecting", "Connecting..", colorConnecting, []string{"Start Time", "Status"}},
		{"connected", "Connected", colorConnected, []string{"Start Time", "Status"}},
		{"disconnected", "Disconnected", colorDisconnected, []string{"Exit Time", "Status", "Play Time"}},
		// An unknown status must not panic or render blank; it reads as the
		// opening phase.
		{"surprise", "Connecting..", colorConnecting, []string{"Start Time", "Status"}},
	} {
		t.Run(test.status, func(t *testing.T) {
			e := ServerLogEmbed(ServerLogEvent{
				PlayerName: "SOT - Ayvix", Username: "Kenji Nakamura",
				Status: test.status, OccurredAt: end, StartedAt: start,
			}, 1)

			if e.Color != test.wantColor {
				t.Fatalf("Color = %#x, want %#x", e.Color, test.wantColor)
			}
			if len(e.Fields) != len(test.wantFields) {
				t.Fatalf("fields = %d, want %d", len(e.Fields), len(test.wantFields))
			}
			for i, name := range test.wantFields {
				if e.Fields[i].Name != name {
					t.Fatalf("field %d = %q, want %q", i, e.Fields[i].Name, name)
				}
			}
			if e.Fields[1].Value != test.wantStatus {
				t.Fatalf("Status = %q, want %q", e.Fields[1].Value, test.wantStatus)
			}
		})
	}
}

// A disconnect reports how long the visit lasted, derived from the visit's
// first event rather than a stored column.
func TestServerLogEmbedPlaytime(t *testing.T) {
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	e := ServerLogEmbed(ServerLogEvent{
		PlayerName: "n", Username: "u", Status: "disconnected",
		OccurredAt: start.Add(110 * time.Minute), StartedAt: start,
	}, 0)

	if got := e.Fields[2].Value; got != "1h 50m" {
		t.Fatalf("Play Time = %q, want %q", got, "1h 50m")
	}
}
