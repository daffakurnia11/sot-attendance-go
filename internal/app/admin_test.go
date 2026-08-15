package app

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMatchingRoleMemberIDs(t *testing.T) {
	members := []*discordgo.Member{
		{User: &discordgo.User{ID: "admin"}, Roles: []string{"member", "admin-role"}},
		{User: &discordgo.User{ID: "other"}, Roles: []string{"member"}},
		{User: &discordgo.User{ID: "bot", Bot: true}, Roles: []string{"admin-role"}},
		nil,
	}
	if got := matchingRoleMemberIDs(members, []string{"owner-role", "admin-role"}); !reflect.DeepEqual(got, []string{"admin"}) {
		t.Fatalf("matchingRoleMemberIDs() = %#v", got)
	}
}

func TestMatchingRoleMembersReturnsConfiguredMemberRoleOnly(t *testing.T) {
	members := []*discordgo.Member{
		{User: &discordgo.User{ID: "member"}, Roles: []string{"member-role"}},
		{User: &discordgo.User{ID: "other"}, Roles: []string{"other-role"}},
	}
	got := matchingRoleMembers(members, []string{"member-role"})
	if len(got) != 1 || got[0].User.ID != "member" {
		t.Fatalf("matchingRoleMembers() = %#v", got)
	}
	if got := matchingRoleMembers(members, []string{""}); len(got) != 0 {
		t.Fatalf("empty role matched %#v", got)
	}
}
