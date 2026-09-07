package presence

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestServerLogEmbedShape(t *testing.T) {
	t.Parallel()
	occurred := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	embed := ServerLogEmbed(ServerLogEvent{
		PlayerName: "Kenji Nakamura",
		Username:   "SOT - Ayvix",
		Status:     "connected",
		OccurredAt: occurred,
		ServerID:   "479",
	})

	if embed.Title != "Kenji Nakamura" {
		t.Errorf("Title = %q", embed.Title)
	}
	if embed.Footer != nil {
		t.Errorf("Footer = %#v, want none", embed.Footer)
	}
	// The embed timestamp is what Discord renders as "Today at 08.03"; the body
	// carries a dated Discord timestamp instead.
	if embed.Timestamp != "" {
		t.Errorf("Timestamp = %q, want empty", embed.Timestamp)
	}
	if len(embed.Fields) != 0 {
		t.Errorf("fields = %#v, want none", embed.Fields)
	}
	want := fmt.Sprintf("```\n[479] SOT - Ayvix is connected.\n```\n%s", discordTimestamp(occurred))
	if embed.Description != want {
		t.Errorf("Description = %q, want %q", embed.Description, want)
	}
}

func TestServerLogEmbedPerStatus(t *testing.T) {
	t.Parallel()
	occurred := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name      string
		event     ServerLogEvent
		wantLine  string
		wantColor int
	}{
		{
			name:      "connecting omits the slot it does not yet own",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "connecting", ServerID: "66299"},
			wantLine:  "SOT - Ayvix is connecting..",
			wantColor: colorConnecting,
		},
		{
			name:      "connected",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "connected", ServerID: "479"},
			wantLine:  "[479] SOT - Ayvix is connected.",
			wantColor: colorConnected,
		},
		{
			name:      "disconnected",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "disconnected", ServerID: "479", Reason: "Exiting"},
			wantLine:  "[479] SOT - Ayvix is exiting. Reason: Exiting",
			wantColor: colorDisconnected,
		},
		{
			name:      "disconnected without a reason",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "disconnected", ServerID: "479"},
			wantLine:  "[479] SOT - Ayvix is exiting.",
			wantColor: colorDisconnected,
		},
		{
			name:      "connected without a server id",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "connected"},
			wantLine:  "SOT - Ayvix is connected.",
			wantColor: colorConnected,
		},
		{
			// An unknown status must not panic or render blank; it reads as the
			// opening phase.
			name:      "unknown status",
			event:     ServerLogEvent{Username: "SOT - Ayvix", Status: "surprise"},
			wantLine:  "SOT - Ayvix is connecting..",
			wantColor: colorConnecting,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.event.OccurredAt = occurred
			e := ServerLogEmbed(test.event)

			if e.Color != test.wantColor {
				t.Errorf("Color = %#x, want %#x", e.Color, test.wantColor)
			}
			want := fmt.Sprintf("```\n%s\n```\n%s", test.wantLine, discordTimestamp(occurred))
			if e.Description != want {
				t.Errorf("Description = %q, want %q", e.Description, want)
			}
		})
	}
}

// FiveM timeout reasons arrive as multi-line command dumps, and a reason is
// never validated on the way in. Neither may break out of the fence.
func TestServerLogEmbedFlattensHostileReason(t *testing.T) {
	t.Parallel()
	e := ServerLogEmbed(ServerLogEvent{
		PlayerName: "Kenji Nakamura",
		Username:   "SOT - Ayvix",
		Status:     "disconnected",
		ServerID:   "479",
		Reason:     "Server->client connection timed out.\nCommand list:\n```QBCore:Player:SetPlayerData (2862 B)",
		OccurredAt: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
	})

	line := strings.Split(e.Description, "\n")[1]
	if strings.Contains(line, "`") {
		t.Errorf("line kept a backtick: %q", line)
	}
	want := "[479] SOT - Ayvix is exiting. Reason: Server->client connection timed out. Command list: '''QBCore:Player:SetPlayerData (2862 B)"
	if line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
	// Exactly one opening fence and one closing fence.
	if got := strings.Count(e.Description, "```"); got != 2 {
		t.Errorf("fences = %d, want 2", got)
	}
}
