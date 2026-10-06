package app

import (
	"github.com/bwmarrin/discordgo"
	"testing"
)

func TestRecapRequiresConfiguredRole(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured []string
		member     *discordgo.Member
		want       bool
	}{
		{"matching", []string{"admin", "other"}, &discordgo.Member{Roles: []string{"other"}}, true},
		{"unrelated", []string{"admin"}, &discordgo.Member{Roles: []string{"member"}}, false},
		{"missing member", []string{"admin"}, nil, false},
		{"no configured roles", nil, &discordgo.Member{Roles: []string{"admin"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bot{adminRoleIDs: tc.configured}
			if got := b.canRunRecap(tc.member); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
