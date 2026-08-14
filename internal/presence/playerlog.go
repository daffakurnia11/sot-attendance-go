package presence

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

const (
	phaseConnecting   playerPhase = "Connecting.."
	phaseConnected    playerPhase = "Connected"
	phaseDisconnected playerPhase = "Disconnected"

	colorConnecting   = 0xFEE75C
	colorConnected    = 0x57F287
	colorDisconnected = 0xED4245
)

type playerPhase string

type playerSession struct {
	phase        playerPhase
	startedAt    time.Time
	missingSince time.Time
}

type playerEvent struct {
	member     *discordgo.Member
	phase      playerPhase
	startedAt  time.Time
	occurredAt time.Time
}

type playerLogger struct {
	channelID       string
	serverName      string
	logger          *slog.Logger
	disconnectGrace time.Duration
	blacklisted     map[string]struct{}
	members         *member.Repository

	mu       sync.Mutex
	sessions map[string]playerSession
}

func newPlayerLogger(channelID, serverName string, disconnectGrace time.Duration, blacklistedUserIDs []string, members *member.Repository, logger *slog.Logger) *playerLogger {
	blacklisted := make(map[string]struct{}, len(blacklistedUserIDs))
	for _, userID := range blacklistedUserIDs {
		blacklisted[userID] = struct{}{}
	}
	return &playerLogger{
		channelID:       channelID,
		serverName:      serverName,
		logger:          logger,
		disconnectGrace: disconnectGrace,
		blacklisted:     blacklisted,
		members:         members,
		sessions:        make(map[string]playerSession),
	}
}

func (l *playerLogger) refresh(session *discordgo.Session, guild *discordgo.Guild, playerCount int, now time.Time) {
	events := l.transitions(guild, now)
	for _, event := range events {
		if l.members != nil {
			firstConnectedAt := event.startedAt
			if firstConnectedAt.IsZero() {
				firstConnectedAt = event.occurredAt
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := l.members.RecordLog(ctx, member.PlayerLog{
				Player: member.Player{
					UserID: event.member.User.ID, Username: event.member.User.Username,
					DisplayName: event.member.DisplayName(), FirstConnectedAt: firstConnectedAt,
				},
				Status: normalizedPlayerPhase(event.phase), StartedAt: nullableTime(event.startedAt),
				OccurredAt: event.occurredAt, Playtime: eventPlaytime(event),
			})
			cancel()
			if err != nil {
				l.logger.Error("persist player activity log", "user_id", event.member.User.ID, "status", event.phase, "error", err)
			}
		}
		if _, err := session.ChannelMessageSendEmbed(l.channelID, playerLogEmbed(event, l.serverName, playerCount)); err != nil {
			l.logger.Error("send player activity log", "channel_id", l.channelID, "user_id", event.member.User.ID, "status", event.phase, "error", err)
			continue
		}
		l.logger.Info("player activity logged", "channel_id", l.channelID, "user_id", event.member.User.ID, "status", event.phase)
	}
}

func normalizedPlayerPhase(phase playerPhase) string {
	return strings.TrimSuffix(strings.ToLower(string(phase)), "..")
}

func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func eventPlaytime(event playerEvent) *time.Duration {
	if event.phase != phaseDisconnected || event.startedAt.IsZero() || event.occurredAt.Before(event.startedAt) {
		return nil
	}
	duration := event.occurredAt.Sub(event.startedAt)
	return &duration
}

func (l *playerLogger) transitions(guild *discordgo.Guild, now time.Time) []playerEvent {
	l.mu.Lock()
	defer l.mu.Unlock()

	members := make(map[string]*discordgo.Member, len(guild.Members))
	for _, member := range guild.Members {
		if member != nil && member.User != nil && !member.User.Bot {
			if _, blocked := l.blacklisted[member.User.ID]; blocked {
				continue
			}
			members[member.User.ID] = member
		}
	}

	seen := make(map[string]struct{}, len(guild.Presences))
	events := make([]playerEvent, 0)
	for _, presence := range guild.Presences {
		if presence == nil || presence.User == nil || presence.User.Bot {
			continue
		}
		member := members[presence.User.ID]
		if member == nil {
			continue
		}
		seen[presence.User.ID] = struct{}{}

		phase, activity := activityPhase(presence, l.serverName)
		previous, tracked := l.sessions[presence.User.ID]
		if phase == phaseDisconnected {
			if tracked && previous.phase != phaseDisconnected {
				if previous.missingSince.IsZero() {
					previous.missingSince = now
					l.sessions[presence.User.ID] = previous
					continue
				}
				if now.Sub(previous.missingSince) >= l.disconnectGrace {
					events = append(events, playerEvent{member: member, phase: phase, startedAt: previous.startedAt, occurredAt: now})
					l.sessions[presence.User.ID] = playerSession{phase: phase}
				}
			}
			continue
		}

		startedAt := activityStart(activity, now)
		if phase == phaseConnected && startedAt.IsZero() && tracked {
			startedAt = previous.startedAt
		}
		if !tracked || previous.phase != phase {
			events = append(events, playerEvent{member: member, phase: phase, startedAt: startedAt, occurredAt: now})
		}
		l.sessions[presence.User.ID] = playerSession{phase: phase, startedAt: startedAt}
	}

	for userID, previous := range l.sessions {
		if _, found := seen[userID]; found || previous.phase == phaseDisconnected {
			continue
		}
		member := members[userID]
		if member != nil {
			if previous.missingSince.IsZero() {
				previous.missingSince = now
				l.sessions[userID] = previous
				continue
			}
			if now.Sub(previous.missingSince) < l.disconnectGrace {
				continue
			}
			events = append(events, playerEvent{member: member, phase: phaseDisconnected, startedAt: previous.startedAt, occurredAt: now})
		}
		l.sessions[userID] = playerSession{phase: phaseDisconnected}
	}

	return events
}

func activityPhase(presence *discordgo.Presence, serverName string) (playerPhase, *discordgo.Activity) {
	if presence.Status == discordgo.StatusOffline || presence.Status == discordgo.StatusInvisible {
		return phaseDisconnected, nil
	}
	if activity := connectingActivity(presence.Activities, serverName); activity != nil {
		return phaseConnecting, activity
	}
	if activity := MatchingActivity(presence.Activities, serverName); activity != nil {
		return phaseConnected, activity
	}
	return phaseDisconnected, nil
}

func connectingActivity(activities []*discordgo.Activity, serverName string) *discordgo.Activity {
	target := normalizeActivityText(serverName)
	if target == "" {
		return nil
	}
	for _, activity := range activities {
		if activity == nil || normalizeActivityText(activity.Name) != "fivem" ||
			!strings.Contains(normalizeActivityText(activity.Details), "connecting") {
			continue
		}
		if strings.Contains(normalizeActivityText(activity.State), target) ||
			strings.Contains(normalizeActivityText(activity.Details), target) {
			return activity
		}
	}
	return nil
}

func activityStart(activity *discordgo.Activity, now time.Time) time.Time {
	if activity == nil || activity.Timestamps.StartTimestamp <= 0 {
		return time.Time{}
	}
	startedAt := time.UnixMilli(activity.Timestamps.StartTimestamp)
	if startedAt.After(now) {
		return time.Time{}
	}
	return startedAt
}

func playerLogEmbed(event playerEvent, serverName string, playerCount int) *discordgo.MessageEmbed {
	title := fmt.Sprintf("%s (@%s)", event.member.DisplayName(), event.member.User.Username)
	builder := embed.New(title).
		Description("**" + serverName + "**").
		Color(playerPhaseColor(event.phase))

	if event.phase == phaseDisconnected {
		builder.Field("Exit Time", discordTimestamp(event.occurredAt), true).
			Field("Status", string(event.phase), true).
			Field("Play Time", elapsedPlaytime(event.startedAt, event.occurredAt), true)
	} else {
		builder.Field("Start Time", availableDiscordTimestamp(event.startedAt), true).
			Field("Status", string(event.phase), true)
	}

	return builder.Footer(fmt.Sprintf("SOT Players: %d", playerCount), "").Timestamp(event.occurredAt).Build()
}

func playerPhaseColor(phase playerPhase) int {
	switch phase {
	case phaseConnecting:
		return colorConnecting
	case phaseConnected:
		return colorConnected
	default:
		return colorDisconnected
	}
}

func availableDiscordTimestamp(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "Unavailable"
	}
	return discordTimestamp(timestamp)
}

func discordTimestamp(timestamp time.Time) string {
	return fmt.Sprintf("<t:%d:F>", timestamp.Unix())
}

func elapsedPlaytime(startedAt, endedAt time.Time) string {
	if startedAt.IsZero() || endedAt.Before(startedAt) {
		return "Unavailable"
	}
	duration := endedAt.Sub(startedAt).Truncate(time.Minute)
	hours := int(duration / time.Hour)
	minutes := int(duration%time.Hour) / int(time.Minute)
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
