package presence

import (
	"io"
	"log/slog"
	"testing"
	"time"

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
	counter := NewCounter("guild", "CR Roleplay", "channel", "", time.Second, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
		{name: "details alone ignored", activities: []*discordgo.Activity{{Name: "FiveM", Details: "Playing CR Roleplay"}}},
		{name: "state alone ignored", activities: []*discordgo.Activity{{Name: "FiveM", State: "CR Roleplay - Downtown"}}},
		{name: "different server", activities: []*discordgo.Activity{{Name: "FiveM", Details: "Other Roleplay"}}},
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
