package payslip

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/bwmarrin/discordgo"
)

// GuildMemberRoles is live guild membership, independent of authentication rows.
type GuildMemberRoles struct {
	Roles    []string `json:"roles"`
	Excluded bool     `json:"excluded"`
}

type GuildRoleReader struct {
	session      *discordgo.Session
	guildID      string
	guestRoleIDs []string
}

func NewGuildRoleReader(client *http.Client, token, guildID string, guestRoleIDs []string) (*GuildRoleReader, error) {
	if token == "" || guildID == "" {
		return nil, fmt.Errorf("Discord bot token and guild ID are required for payslip roles")
	}
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create guild role reader: %w", err)
	}
	session.Client = client
	return &GuildRoleReader{session: session, guildID: guildID, guestRoleIDs: guestRoleIDs}, nil
}

func (r *GuildRoleReader) Load(ctx context.Context) (map[string]GuildMemberRoles, error) {
	roles, err := r.session.GuildRoles(r.guildID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("load guild roles: %w", err)
	}
	names := make(map[string]string, len(roles))
	for _, role := range roles {
		if role.ID == "1526876863329341530" || slices.Contains(r.guestRoleIDs, role.ID) {
			names[role.ID] = role.Name
		}
	}
	result := make(map[string]GuildMemberRoles)
	after := ""
	for {
		members, err := r.session.GuildMembers(r.guildID, after, 1000, discordgo.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("load guild members: %w", err)
		}
		for _, member := range members {
			if member.User == nil {
				return nil, fmt.Errorf("guild member missing identity")
			}
			entry := GuildMemberRoles{Roles: []string{}}
			for _, id := range member.Roles {
				if name, ok := names[id]; ok {
					entry.Roles = append(entry.Roles, name)
				}
				if slices.Contains(r.guestRoleIDs, id) {
					entry.Excluded = true
				}
			}
			result[member.User.ID] = entry
		}
		if len(members) < 1000 {
			break
		}
		next := members[len(members)-1].User.ID
		if next == after {
			return nil, fmt.Errorf("guild member pagination did not advance")
		}
		after = next
	}
	return result, nil
}
