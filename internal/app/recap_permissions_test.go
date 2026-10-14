package app

import (
	"github.com/bwmarrin/discordgo"
	"testing"
)

func TestManualRecapRedirectsAllRolesWithoutDatabase(t *testing.T) {
	b := &Bot{playerRecapChannelID: "987"}
	want := "Manual recap commands are disabled. Open <#987> to see the attendance recap."
	for _, roles := range [][]string{nil, {"member"}, {"admin"}} {
		interaction := &discordgo.Interaction{Member: &discordgo.Member{User: &discordgo.User{ID: "123"}, Roles: roles}}
		response, userID, err := b.slashCommandResponse(interaction, "recap")
		if err != nil || userID != "123" || response == nil || response.Content == nil || *response.Content != want {
			t.Fatalf("roles=%v: response=%+v user=%s error=%v", roles, response, userID, err)
		}
	}
	if got := b.recapChannelResponse(); got != want {
		t.Fatalf("prefix response=%q", got)
	}
}

func TestRecapAllowedInLocalReadOnlyMode(t *testing.T) {
	for _, name := range []string{"recap", "check", "status"} {
		if !isReadOnlyCommand(name) {
			t.Errorf("%s should be allowed", name)
		}
	}
	for _, name := range []string{"craft", "money", "stash", "unknown"} {
		if isReadOnlyCommand(name) {
			t.Errorf("%s should stay blocked", name)
		}
	}
}
