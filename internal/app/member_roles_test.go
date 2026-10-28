package app

import (
	"github.com/bwmarrin/discordgo"
	"testing"
)

func TestRoleSnapshot(t *testing.T) {
	roles := []*discordgo.Role{{ID: "cr", Name: "SOT CR"}, {ID: "guest", Name: "SOT CR Guest"}, {ID: "other", Name: "UNDERBOSS"}}
	people := []*discordgo.Member{
		{User: &discordgo.User{ID: "1"}, Roles: []string{"cr"}},
		{User: &discordgo.User{ID: "2"}, Roles: []string{"cr", "guest"}},
		{User: &discordgo.User{ID: "3"}, Roles: []string{"other"}},
		{User: &discordgo.User{ID: "4", Bot: true}, Roles: []string{"cr"}},
	}
	got := roleSnapshot(people, roles)
	if len(got) != 3 || got[0].ID != "cr" || got[0].Name != "SOT CR" || got[1].ID != "guest" || got[1].Name != "SOT CR Guest" || got[2].ID != "" || got[2].Name != "" {
		t.Fatalf("incorrect roles: %+v", got)
	}
}
