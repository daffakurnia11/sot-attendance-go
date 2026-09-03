package presence

import (
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestPlayerLoggerInitializationSuppressesExistingActivity(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	start := time.Date(2026, time.August, 14, 14, 0, 0, 0, time.UTC)
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{{User: &discordgo.User{ID: "player", Username: "player"}}},
		Presences: []*discordgo.Presence{presence("player", discordgo.StatusOnline, &discordgo.Activity{
			Name: "CR Roleplay", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()},
		})},
	}

	logger.refresh(nil, guild, 1, start.Add(time.Hour))
	if !logger.initialized || len(logger.sessions) != 1 {
		t.Fatalf("startup baseline = (initialized: %v, tracked: %d), want (true, 1)", logger.initialized, len(logger.sessions))
	}
	if events := logger.transitions(guild, start.Add(time.Hour)); len(events) != 0 {
		t.Fatalf("startup activity emitted %d events", len(events))
	}

	guild.Presences[0].Status = discordgo.StatusOffline
	if events := logger.transitions(guild, start.Add(time.Hour+time.Second)); len(events) != 0 {
		t.Fatalf("first missing poll emitted %d events", len(events))
	}
	events := logger.transitions(guild, start.Add(time.Hour+16*time.Second))
	assertEvent(t, events, phaseDisconnected)
	if !events[0].startedAt.Equal(start) {
		t.Errorf("disconnected start = %v, want %v", events[0].startedAt, start)
	}
}

func TestPlayerLoggerBaselineReportsActivePlayers(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	start := time.Date(2026, time.August, 14, 14, 0, 0, 0, time.UTC)
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{
			{User: &discordgo.User{ID: "playing", Username: "playing"}},
			{User: &discordgo.User{ID: "idle", Username: "idle"}},
		},
		Presences: []*discordgo.Presence{
			presence("playing", discordgo.StatusOnline, &discordgo.Activity{
				Name: "CR Roleplay", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()},
			}),
			presence("idle", discordgo.StatusOnline, &discordgo.Activity{Name: "Visual Studio Code"}),
		},
	}

	initialized, tracked, activeUserIDs := logger.initialize(guild, start)
	if !initialized || tracked != 1 {
		t.Fatalf("baseline = (initialized: %v, tracked: %d), want (true, 1)", initialized, tracked)
	}
	// Only the FiveM player counts as active. Reconciliation closes every
	// recorded session outside this set, so a member listed here in error would
	// keep a stale row open, and one omitted in error would be disconnected
	// mid-session.
	if len(activeUserIDs) != 1 || activeUserIDs[0] != "playing" {
		t.Fatalf("active players = %v, want [playing]", activeUserIDs)
	}

	if _, _, repeated := logger.initialize(guild, start); repeated != nil {
		t.Errorf("second baseline reported %v active players, want none", repeated)
	}
}

func TestPlayerLoggerInitializationIsAtomic(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	guild := &discordgo.Guild{}
	var initializedCount atomic.Int32
	var waitGroup sync.WaitGroup
	for range 10 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if initialized, _, _ := logger.initialize(guild, time.Now()); initialized {
				initializedCount.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if got := initializedCount.Load(); got != 1 {
		t.Fatalf("initialize succeeded %d times, want 1", got)
	}
}

func TestPlayerLoggerTransitions(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	member := &discordgo.Member{Nick: "DeltaKilo", User: &discordgo.User{ID: "player", Username: "deltakilo11"}}
	start := time.Date(2026, time.August, 11, 16, 40, 0, 0, time.UTC)
	guild := &discordgo.Guild{Members: []*discordgo.Member{member}}

	guild.Presences = []*discordgo.Presence{presence("player", discordgo.StatusOnline,
		&discordgo.Activity{Name: "FiveM", Details: "Connecting...", State: "CR ROLEPLAY INDONESIA", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()}},
	)}
	events := logger.transitions(guild, start.Add(time.Minute))
	assertEvent(t, events, phaseConnecting)

	if events := logger.transitions(guild, start.Add(2*time.Minute)); len(events) != 0 {
		t.Fatalf("unchanged connecting state emitted %d events", len(events))
	}

	guild.Presences[0].Activities[0] = &discordgo.Activity{Name: "CR Roleplay", Details: "Players 95/1000", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()}}
	events = logger.transitions(guild, start.Add(3*time.Minute))
	assertEvent(t, events, phaseConnected)
	if !events[0].startedAt.Equal(start) {
		t.Errorf("connected start = %v, want %v", events[0].startedAt, start)
	}

	guild.Presences[0].Status = discordgo.StatusOffline
	events = logger.transitions(guild, start.Add(2*time.Hour+5*time.Minute))
	if len(events) != 0 {
		t.Fatalf("first missing poll emitted %d events", len(events))
	}
	events = logger.transitions(guild, start.Add(2*time.Hour+5*time.Minute+15*time.Second))
	assertEvent(t, events, phaseDisconnected)
	if !events[0].startedAt.Equal(start) {
		t.Errorf("disconnected start = %v, want %v", events[0].startedAt, start)
	}

	if events := logger.transitions(guild, start.Add(3*time.Hour)); len(events) != 0 {
		t.Fatalf("unchanged disconnected state emitted %d events", len(events))
	}
}

func TestPlayerLoggerSkipsInitiallyInactiveMember(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{{User: &discordgo.User{ID: "player", Username: "player"}}},
		Presences: []*discordgo.Presence{presence("player", discordgo.StatusOnline,
			&discordgo.Activity{Name: "Visual Studio Code"},
		)},
	}
	if events := logger.transitions(guild, time.Now()); len(events) != 0 {
		t.Fatalf("inactive member emitted %d events", len(events))
	}
}

func TestPlayerLoggerIgnoresGapBetweenConnectingAndConnected(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	member := &discordgo.Member{User: &discordgo.User{ID: "player", Username: "player"}}
	start := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{member},
		Presences: []*discordgo.Presence{presence("player", discordgo.StatusOnline,
			&discordgo.Activity{Name: "FiveM", Details: "Connecting...", State: "CR ROLEPLAY INDONESIA", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()}},
		)},
	}
	assertEvent(t, logger.transitions(guild, start), phaseConnecting)

	guild.Presences[0].Activities = nil
	if events := logger.transitions(guild, start.Add(5*time.Second)); len(events) != 0 {
		t.Fatalf("activity replacement gap emitted %d events", len(events))
	}

	guild.Presences[0].Activities = []*discordgo.Activity{{
		Name: "CR Roleplay", Details: "Players 95/1000", Timestamps: discordgo.TimeStamps{StartTimestamp: start.UnixMilli()},
	}}
	events := logger.transitions(guild, start.Add(10*time.Second))
	assertEvent(t, events, phaseConnected)
	for _, event := range events {
		if event.phase == phaseDisconnected {
			t.Error("gap must not emit disconnected event")
		}
	}
}

func TestActivityPhaseUsesDiscordActivitySignatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		presence *discordgo.Presence
		want     playerPhase
	}{
		{
			name: "connecting screenshot",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "FiveM", Details: "Connecting...", State: "CR ROLEPLAY INDONESIA",
			}),
			want: phaseConnecting,
		},
		{
			name: "connected screenshot",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "CR Roleplay", Details: "Players 95/1000",
			}),
			want: phaseConnected,
		},
		{
			name: "unrelated activity",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "Visual Studio Code", Details: "Editing code",
			}),
			want: phaseDisconnected,
		},
		{
			name: "fivem without connecting signature",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "FiveM", Details: "Main menu", State: "CR ROLEPLAY INDONESIA",
			}),
			want: phaseDisconnected,
		},
		{
			name: "connecting to different server",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "FiveM", Details: "Connecting...", State: "OTHER ROLEPLAY",
			}),
			want: phaseDisconnected,
		},
		{
			name: "connecting without server identity",
			presence: presence("player", discordgo.StatusOnline, &discordgo.Activity{
				Name: "FiveM", Details: "Connecting...",
			}),
			want: phaseDisconnected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := activityPhase(tt.presence, "CR Roleplay")
			if got != tt.want {
				t.Errorf("activityPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlayerLoggerSkipsBlacklistedUsers(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "", 15*time.Second, []string{"blocked"}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{
			{User: &discordgo.User{ID: "blocked", Username: "blocked"}},
			{User: &discordgo.User{ID: "allowed", Username: "allowed"}},
		},
		Presences: []*discordgo.Presence{
			presence("blocked", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("allowed", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
		},
	}

	events := logger.transitions(guild, time.Now())
	if len(events) != 1 || events[0].member.User.ID != "allowed" {
		t.Fatalf("events = %#v, want allowed user only", events)
	}
	if _, tracked := logger.sessions["blocked"]; tracked {
		t.Error("blacklisted user must not enter transition state")
	}
	if got := len(matchingMemberIDs(guild, "CR Roleplay", "")); got != 2 {
		t.Errorf("status count = %d, want blacklisted user included", got)
	}
}

func TestPlayerLoggerSkipsMembersWithoutRequiredRole(t *testing.T) {
	t.Parallel()

	logger := newPlayerLogger("channel", "CR Roleplay", "member-role", 15*time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	guild := &discordgo.Guild{
		Members: []*discordgo.Member{
			{User: &discordgo.User{ID: "eligible", Username: "eligible"}, Roles: []string{"member-role"}},
			{User: &discordgo.User{ID: "excluded", Username: "excluded"}},
		},
		Presences: []*discordgo.Presence{
			presence("eligible", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("excluded", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
		},
	}

	events := logger.transitions(guild, time.Now())
	if len(events) != 1 || events[0].member.User.ID != "eligible" {
		t.Fatalf("events = %#v, want eligible member only", events)
	}
	if _, tracked := logger.sessions["excluded"]; tracked {
		t.Error("member without required role must not enter transition state")
	}
}

func TestPlayerLogEmbed(t *testing.T) {
	t.Parallel()

	member := &discordgo.Member{Nick: "DeltaKilo", User: &discordgo.User{Username: "deltakilo11"}}
	start := time.Date(2026, time.August, 11, 16, 40, 0, 0, time.UTC)
	end := start.Add(2*time.Hour + 5*time.Minute)
	embed := playerLogEmbed(playerEvent{
		member: member, characterName: "Kenji Nakamura", phase: phaseDisconnected, startedAt: start, occurredAt: end,
	}, 0)

	if embed.Title != "Kenji Nakamura (@deltakilo11)" || embed.Description != "" {
		t.Errorf("unexpected header: %#v", embed)
	}
	if embed.Color != colorDisconnected || len(embed.Fields) != 3 {
		t.Errorf("unexpected disconnected layout: %#v", embed)
	}
	if embed.Fields[2].Name != "Play Time" || embed.Fields[2].Value != "2h 5m" {
		t.Errorf("unexpected playtime: %#v", embed.Fields[2])
	}
	if embed.Thumbnail != nil || embed.Image != nil {
		t.Error("player log must not contain avatar or image")
	}
	for _, field := range embed.Fields {
		if field.Name == "In Character Name" {
			t.Error("player log must not contain IC name")
		}
	}
	if embed.Footer == nil || embed.Footer.Text != "SOT Players: 0 • Discord Activity" {
		t.Errorf("unexpected footer: %#v", embed.Footer)
	}
	if embed.Timestamp != end.Format(time.RFC3339) {
		t.Errorf("Timestamp = %q", embed.Timestamp)
	}
}

func TestPlayerLogEmbedFallsBackToDisplayName(t *testing.T) {
	t.Parallel()

	member := &discordgo.Member{Nick: "DeltaKilo", User: &discordgo.User{Username: "deltakilo11"}}
	embed := playerLogEmbed(playerEvent{member: member, characterName: "  ", phase: phaseConnected}, 1)
	if embed.Title != "DeltaKilo (@deltakilo11)" {
		t.Errorf("Title = %q, want display-name fallback", embed.Title)
	}
}

func TestPlayerLogEmbedConnecting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 11, 16, 40, 0, 0, time.UTC)
	member := &discordgo.Member{User: &discordgo.User{Username: "player"}}
	embed := playerLogEmbed(playerEvent{member: member, phase: phaseConnecting, occurredAt: now}, 1)
	if embed.Color != colorConnecting || len(embed.Fields) != 2 {
		t.Errorf("unexpected connecting layout: %#v", embed)
	}
	if embed.Fields[0].Value != "Unavailable" || embed.Fields[1].Value != "Connecting.." {
		t.Errorf("unexpected connecting fields: %#v", embed.Fields)
	}
}

func assertEvent(t *testing.T, events []playerEvent, phase playerPhase) {
	t.Helper()
	if len(events) != 1 || events[0].phase != phase {
		t.Fatalf("events = %#v, want one %s event", events, phase)
	}
}
