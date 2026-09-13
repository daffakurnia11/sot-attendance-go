package presence

import (
	"io"
	"log/slog"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestCountMatchingMembers(t *testing.T) {
	t.Parallel()

	guild := &discordgo.Guild{
		Members: []*discordgo.Member{
			{User: &discordgo.User{ID: "playing"}},
			{User: &discordgo.User{ID: "details"}},
			{User: &discordgo.User{ID: "offline"}},
			{User: &discordgo.User{ID: "invisible"}},
			{User: &discordgo.User{ID: "other"}},
			{User: &discordgo.User{ID: "bot", Bot: true}},
		},
		Presences: []*discordgo.Presence{
			presence("playing", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("details", discordgo.StatusIdle, &discordgo.Activity{Name: "CR: Roleplay | City"}),
			presence("offline", discordgo.StatusOffline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("invisible", discordgo.StatusInvisible, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("other", discordgo.StatusDoNotDisturb, &discordgo.Activity{Name: "Other City"}),
			presence("bot", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("uncached-member", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
		},
	}

	if got := len(matchingMemberIDs(guild, "CR Roleplay", "")); got != 3 {
		t.Errorf("matchingMemberIDs() count = %d, want 3", got)
	}
}

func TestCounterCountStartsUnavailable(t *testing.T) {
	t.Parallel()
	counter := NewCounter("guild", "CR Roleplay", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if count, available := counter.Count(); available || count != -1 {
		t.Fatalf("Count() = %d, %v; want -1, false", count, available)
	}
}

func TestCountMatchingMembersWithRequiredRole(t *testing.T) {
	t.Parallel()

	guild := &discordgo.Guild{
		Members: []*discordgo.Member{
			{User: &discordgo.User{ID: "eligible"}, Roles: []string{"member-role"}},
			{User: &discordgo.User{ID: "other-role"}, Roles: []string{"other-role"}},
			{User: &discordgo.User{ID: "no-role"}},
		},
		Presences: []*discordgo.Presence{
			presence("eligible", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("other-role", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
			presence("no-role", discordgo.StatusOnline, &discordgo.Activity{Name: "CR Roleplay"}),
		},
	}

	playing := matchingMemberIDs(guild, "CR Roleplay", "member-role")
	if len(playing) != 1 {
		t.Fatalf("matchingMemberIDs() = %#v, want eligible member only", playing)
	}
	if _, found := playing["eligible"]; !found {
		t.Errorf("matchingMemberIDs() = %#v, want eligible member", playing)
	}
}

func TestHasMatchingActivity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		activities []*discordgo.Activity
		want       bool
	}{
		{name: "name exact", activities: []*discordgo.Activity{{Name: "CR Roleplay"}}, want: true},
		{name: "case and punctuation", activities: []*discordgo.Activity{{Name: "cr-roleplay"}}, want: true},
		// Name alone used to be the only field searched. FiveM names the
		// launcher there while joining and puts the server in State, so a
		// player was invisible to the bot for the whole time they connected.
		{name: "details name the server", activities: []*discordgo.Activity{{Name: "FiveM", Details: "Playing CR Roleplay"}}, want: true},
		{name: "state names the server", activities: []*discordgo.Activity{{Name: "FiveM", State: "CR Roleplay - Downtown"}}, want: true},
		{name: "joining, as the client reports it", activities: []*discordgo.Activity{{Name: "FiveM", Details: "Connecting...", State: "CR ROLEPLAY INDONESIA"}}, want: true},
		// In-game, FiveM can move the server name into the icon hover text and
		// leave Details and State describing the character instead. Losing the
		// match partway through a visit reads exactly like the player leaving.
		{name: "server named only in the icon hover text", activities: []*discordgo.Activity{{Name: "FiveM", Details: "SOT - Ayvix", State: "Los Santos", Assets: discordgo.Assets{LargeText: "CR Roleplay Indonesia"}}}, want: true},
		{name: "server named only in the small icon text", activities: []*discordgo.Activity{{Name: "FiveM", Assets: discordgo.Assets{SmallText: "CR Roleplay"}}}, want: true},
		// Widening the search must not start matching another server.
		{name: "different server", activities: []*discordgo.Activity{{Name: "FiveM", Details: "Other Roleplay"}}},
		{name: "different server in state", activities: []*discordgo.Activity{{Name: "FiveM", State: "Other Roleplay - Downtown"}}},
		{name: "different server in the hover text", activities: []*discordgo.Activity{{Name: "FiveM", Assets: discordgo.Assets{LargeText: "Other Roleplay"}}}},
		{name: "no activities"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasMatchingActivity(tt.activities, "CR Roleplay"); got != tt.want {
				t.Errorf("hasMatchingActivity() = %v, want %v", got, tt.want)
			}
		})
	}
}

func presence(userID string, status discordgo.Status, activities ...*discordgo.Activity) *discordgo.Presence {
	return &discordgo.Presence{
		User:       &discordgo.User{ID: userID},
		Status:     status,
		Activities: activities,
	}
}

// The activity is one Discord entry but two states of a visit, and only Details
// separates them.
func TestActivityConnecting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		activity *discordgo.Activity
		want     bool
	}{
		{name: "joining", activity: &discordgo.Activity{Name: "FiveM", Details: "Connecting...", State: "CR ROLEPLAY INDONESIA"}, want: true},
		{name: "in the server", activity: &discordgo.Activity{Name: "FiveM", Details: "Downtown", State: "CR ROLEPLAY INDONESIA"}},
		{name: "no details", activity: &discordgo.Activity{Name: "CR Roleplay"}},
		{name: "no activity", activity: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := activityConnecting(test.activity); got != test.want {
				t.Errorf("activityConnecting() = %v, want %v", got, test.want)
			}
		})
	}
}
