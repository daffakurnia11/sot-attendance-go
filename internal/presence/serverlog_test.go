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
		PlayerName:    "Kenji Nakamura",
		Username:      "SOT - Ayvix",
		Status:        "connected",
		OccurredAt:    occurred,
		ServerID:      "479",
		DiscordUserID: "1220326041067982941",
	})

	if embed.Title != "" {
		t.Errorf("Title = %q, want empty", embed.Title)
	}
	if embed.Footer == nil || embed.Footer.Text != "03 September 2026 at 09:00 • Source: CR Roleplay" {
		t.Errorf("Footer = %#v", embed.Footer)
	}
	// The embed timestamp is what Discord renders as "Today at 08.03"; the
	// footer carries a dated time instead.
	if embed.Timestamp != "" {
		t.Errorf("Timestamp = %q, want empty", embed.Timestamp)
	}
	if len(embed.Fields) != 0 {
		t.Errorf("fields = %#v, want none", embed.Fields)
	}
	want := "**Kenji Nakamura** (<@1220326041067982941>)\n```\n[479] SOT - Ayvix is connected.\n```"
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
			wantLine:  "[479] SOT - Ayvix is exiting.\nReason: Exiting",
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
			want := fmt.Sprintf("```\n%s\n```", test.wantLine)
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

	// The reason keeps its own line and stays one line, however many the
	// sender used.
	// Heading, fence, sentence, reason: the reason is the fourth line.
	lines := strings.Split(e.Description, "\n")
	if strings.Contains(lines[3], "`") {
		t.Errorf("reason kept a backtick: %q", lines[3])
	}
	want := "Reason: Server->client connection timed out. Command list: '''QBCore:Player:SetPlayerData (2862 B)"
	if lines[3] != want {
		t.Errorf("reason = %q, want %q", lines[3], want)
	}
	// Exactly one opening fence and one closing fence.
	if got := strings.Count(e.Description, "```"); got != 2 {
		t.Errorf("fences = %d, want 2", got)
	}
}

// The footer reports how long the visit lasted, and only on a disconnect: the
// other two statuses open a visit rather than close one.
func TestServerLogEmbedFooterPlaytime(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name  string
		event ServerLogEvent
		want  string
	}{
		{
			name:  "disconnect reports playtime",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "disconnected", OccurredAt: start.Add(110 * time.Minute), StartedAt: start},
			want:  "03 September 2026 at 10:50 • Playtime: 1h 50m • Source: CR Roleplay",
		},
		{
			name:  "disconnect without a known start",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "disconnected", OccurredAt: start},
			want:  "03 September 2026 at 09:00 • Playtime: Unavailable • Source: CR Roleplay",
		},
		{
			name:  "connected reports none",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "connected", OccurredAt: start, StartedAt: start},
			want:  "03 September 2026 at 09:00 • Source: CR Roleplay",
		},
		{
			name:  "connecting reports none",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "connecting", OccurredAt: start},
			want:  "03 September 2026 at 09:00 • Source: CR Roleplay",
		},
		{
			// Discord sees an activity, not a connection, so a visit inferred
			// from it must never read as a report from the game server.
			name:  "Discord activity is named as the witness",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "connected", OccurredAt: start, Source: "discord"},
			want:  "03 September 2026 at 09:00 • Source: Discord activity",
		},
		{
			// Every row stored before the source column existed came from the
			// webhook, and the column defaulted them to it.
			name:  "an unset source reads as the webhook",
			event: ServerLogEvent{PlayerName: "Prince Lim", Status: "connected", OccurredAt: start},
			want:  "03 September 2026 at 09:00 • Source: CR Roleplay",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			e := ServerLogEmbed(test.event)
			if e.Footer == nil || e.Footer.Text != test.want {
				t.Errorf("Footer = %#v, want %q", e.Footer, test.want)
			}
		})
	}
}

func TestServerLogHeading(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		event ServerLogEvent
		want  string
	}{
		{
			name:  "name and mention",
			event: ServerLogEvent{PlayerName: "Kenji Nakamura", DiscordUserID: "1220326041067982941"},
			want:  "**Kenji Nakamura** (<@1220326041067982941>)",
		},
		{
			name:  "no stored Discord id",
			event: ServerLogEvent{PlayerName: "Kenji Nakamura"},
			want:  "**Kenji Nakamura**",
		},
		{
			// A malformed id would render as literal <@...> text.
			name:  "non-numeric Discord id is not a mention",
			event: ServerLogEvent{PlayerName: "Kenji Nakamura", DiscordUserID: "discord:123"},
			want:  "**Kenji Nakamura**",
		},
		{
			name:  "mention alone",
			event: ServerLogEvent{DiscordUserID: "1220326041067982941"},
			want:  "<@1220326041067982941>",
		},
		{
			name:  "neither",
			event: ServerLogEvent{},
			want:  "",
		},
		{
			// Formatting characters in a display name must not leak out of the
			// bold name and reformat the line.
			name:  "markdown in a display name is escaped",
			event: ServerLogEvent{PlayerName: "**Kenji** _N_"},
			want:  `**\*\*Kenji\*\* \_N\_**`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := serverLogHeading(test.event); got != test.want {
				t.Errorf("serverLogHeading() = %q, want %q", got, test.want)
			}
		})
	}
}

// With no player to name, the body is the reported line alone rather than a
// blank first line.
func TestServerLogEmbedWithoutHeading(t *testing.T) {
	t.Parallel()
	e := ServerLogEmbed(ServerLogEvent{
		Username:   "SOT - Ayvix",
		Status:     "connected",
		ServerID:   "479",
		OccurredAt: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
	})
	if want := "```\n[479] SOT - Ayvix is connected.\n```"; e.Description != want {
		t.Errorf("Description = %q, want %q", e.Description, want)
	}
}
