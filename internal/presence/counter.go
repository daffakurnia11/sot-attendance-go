package presence

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

type Counter struct {
	guildID    string
	serverName string
	roleID     string
	logger     *slog.Logger

	mu        sync.Mutex
	lastCount int
}

// NewCounter builds the status counter.
//
// It no longer takes a log channel, a member repository or a blacklist: the
// Discord activity log it fed - embeds to a channel plus a row per transition
// in activity_logs - is gone. The game server reports the same events over the
// webhook, and duplicating them from a guess about rich presence bought a
// second, worse record. What remains reads the gateway cache and writes
// nothing.
func NewCounter(guildID, serverName, roleID string, logger *slog.Logger) *Counter {
	return &Counter{
		guildID:    guildID,
		serverName: serverName,
		roleID:     roleID,
		logger:     logger,
		lastCount:  -1,
	}
}

func (c *Counter) Refresh(session *discordgo.Session) {
	guild, err := session.State.Guild(c.guildID)
	if err != nil {
		c.logger.Warn("guild unavailable for status counter", "guild_id", c.guildID, "error", err)
		return
	}
	if guild.Unavailable {
		c.logger.Debug("guild unavailable for player baseline", "guild_id", c.guildID)
		return
	}

	count := len(matchingMemberIDs(guild, c.serverName, c.roleID))

	c.mu.Lock()
	defer c.mu.Unlock()
	if count == c.lastCount {
		return
	}
	c.lastCount = count
	c.logger.Info("Discord activity count updated", "guild_id", c.guildID, "server_name", c.serverName, "count", count)
}

func (c *Counter) Count() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastCount, c.lastCount >= 0
}

func (c *Counter) GuildID() string { return c.guildID }

func (c *Counter) ServerName() string { return c.serverName }

func matchingMemberIDs(guild *discordgo.Guild, serverName, roleID string) map[string]struct{} {
	eligible := make(map[string]bool, len(guild.Members))
	for _, member := range guild.Members {
		if member != nil && member.User != nil {
			eligible[member.User.ID] = !member.User.Bot && memberHasRole(member, roleID)
		}
	}

	playing := make(map[string]struct{})
	for _, presence := range guild.Presences {
		if presence == nil || presence.User == nil || presence.User.Bot {
			continue
		}
		isEligible, memberKnown := eligible[presence.User.ID]
		if (roleID != "" && !isEligible) || (roleID == "" && memberKnown && !isEligible) {
			continue
		}
		if presence.Status == discordgo.StatusOffline || presence.Status == discordgo.StatusInvisible {
			continue
		}
		if MatchingActivity(presence.Activities, serverName) != nil {
			playing[presence.User.ID] = struct{}{}
		}
	}
	return playing
}

func memberHasRole(member *discordgo.Member, roleID string) bool {
	if roleID == "" {
		return true
	}
	for _, memberRoleID := range member.Roles {
		if memberRoleID == roleID {
			return true
		}
	}
	return false
}

func hasMatchingActivity(activities []*discordgo.Activity, serverName string) bool {
	return MatchingActivity(activities, serverName) != nil
}

func MatchingActivity(activities []*discordgo.Activity, serverName string) *discordgo.Activity {
	target := normalizeActivityText(serverName)
	if target == "" {
		return nil
	}

	for _, activity := range activities {
		if activity == nil {
			continue
		}
		candidate := normalizeActivityText(activity.Name)
		if strings.Contains(candidate, target) {
			return activity
		}
	}
	return nil
}

func normalizeActivityText(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return unicode.ToLower(char)
		}
		return -1
	}, value)
}

// MemberPresence is one member's live Discord state, as the gateway sees it
// right now.
//
// It is read straight from the gateway cache and never stored. The activity log
// that used to persist these transitions is gone: the game server reports the
// same events over the webhook and reports them better, so presence is only
// worth serving live.
type MemberPresence struct {
	DiscordUserID string     `json:"discord_user_id"`
	Status        string     `json:"status"`
	Playing       bool       `json:"playing"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
}

// Snapshot lists the live Discord presence of every eligible guild member.
//
// Members with no presence entry are omitted rather than reported offline: the
// gateway simply has nothing to say about them, and inventing a status would
// make an absent cache look like a real observation.
func (c *Counter) Snapshot(session *discordgo.Session) ([]MemberPresence, error) {
	guild, err := session.State.Guild(c.guildID)
	if err != nil {
		return nil, fmt.Errorf("guild unavailable for presence snapshot: %w", err)
	}
	if guild.Unavailable {
		return nil, errors.New("guild unavailable for presence snapshot")
	}

	eligible := make(map[string]bool, len(guild.Members))
	for _, member := range guild.Members {
		if member != nil && member.User != nil {
			eligible[member.User.ID] = !member.User.Bot && memberHasRole(member, c.roleID)
		}
	}

	presences := make([]MemberPresence, 0, len(guild.Presences))
	for _, presence := range guild.Presences {
		if presence == nil || presence.User == nil || presence.User.Bot {
			continue
		}
		isEligible, memberKnown := eligible[presence.User.ID]
		if (c.roleID != "" && !isEligible) || (c.roleID == "" && memberKnown && !isEligible) {
			continue
		}

		entry := MemberPresence{
			DiscordUserID: presence.User.ID,
			Status:        string(presence.Status),
		}
		// Offline and invisible are indistinguishable to a bot, and neither can
		// be playing whatever the activity list says.
		if presence.Status != discordgo.StatusOffline && presence.Status != discordgo.StatusInvisible {
			if activity := MatchingActivity(presence.Activities, c.serverName); activity != nil {
				entry.Playing = true
				if activity.Timestamps.StartTimestamp != 0 {
					startedAt := time.UnixMilli(activity.Timestamps.StartTimestamp).UTC()
					entry.StartedAt = &startedAt
				}
			}
		}
		presences = append(presences, entry)
	}
	return presences, nil
}
