package app

import (
	"context"
	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"time"
)

// Guest takes priority when both roles are assigned, preserving the more specific membership classification.
func selectedMemberRole(ids []string, names map[string]string) (string, string) {
	for _, target := range []string{"SOT CR Guest", "SOT CR"} {
		for _, id := range ids {
			if names[id] == target {
				return id, target
			}
		}
	}
	return "", ""
}

func (b *Bot) runMemberRoleSync(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if b.ready.Load() {
				b.syncGuildMembers(b.session)
			}
		}
	}
}

func roleSnapshot(members []*discordgo.Member, roles []*discordgo.Role) []member.Role {
	names := map[string]string{}
	for _, role := range roles {
		if role != nil {
			names[role.ID] = role.Name
		}
	}
	result := []member.Role{}
	for _, m := range members {
		if m == nil || m.User == nil || m.User.Bot {
			continue
		}
		id, name := selectedMemberRole(m.Roles, names)
		result = append(result, member.Role{DiscordUserID: m.User.ID, ID: id, Name: name})
	}
	return result
}
