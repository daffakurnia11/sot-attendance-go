package presence

import (
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

type Counter struct {
	guildID    string
	serverName string
	roleID     string
	logger     *slog.Logger
	playerLog  *playerLogger

	mu        sync.Mutex
	lastCount int
}

func NewCounter(guildID, serverName, playerLogChannelID, roleID string, pollInterval time.Duration, blacklistedUserIDs []string, members *member.Repository, logger *slog.Logger) *Counter {
	disconnectGrace := 2 * pollInterval
	if disconnectGrace < 15*time.Second {
		disconnectGrace = 15 * time.Second
	}
	return &Counter{
		guildID:    guildID,
		serverName: serverName,
		roleID:     roleID,
		logger:     logger,
		playerLog:  newPlayerLogger(playerLogChannelID, serverName, roleID, disconnectGrace, blacklistedUserIDs, members, logger),
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

	playing := matchingMemberIDs(guild, c.serverName, c.roleID)
	count := len(playing)
	c.playerLog.refresh(session, guild, count, time.Now())

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
