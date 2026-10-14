package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
)

const statusCommand = "status"

func connectionStatus(webhook string, activity presence.MemberPresence) string {
	if webhook == "connected" {
		return "Connected"
	}
	if activity.Playing {
		if activity.Connecting {
			return "Connecting"
		}
		return "Connected"
	}
	if webhook == "connecting" {
		return "Connecting"
	}
	return "Not Connected"
}

// The newest event across all characters wins; abandoned sessions must not
// override a later disconnect. ID breaks ties at the same event timestamp.
const latestWebhookStatus = `SELECT COALESCE((
 SELECT CASE WHEN sl.status IN ('connected','connecting')
  AND sl.occurred_at <= NOW()-INTERVAL '12 hours' THEN 'unknown' ELSE sl.status END
 FROM server_logs sl JOIN server_members sm ON sm.id=sl.server_member_id
 WHERE sm.discord_user_id=$1 AND sl.source='server'
 ORDER BY sl.occurred_at DESC, sl.id DESC LIMIT 1
), 'unknown')`

// Read the start only from the latest CR event's session, never an older open visit.
const latestWebhookConnectedAt = `SELECT MIN(s.occurred_at)
 FROM server_logs s JOIN (
  SELECT sl.session_id, sl.server_member_id, sl.status
  FROM server_logs sl JOIN server_members sm ON sm.id=sl.server_member_id
  WHERE sm.discord_user_id=$1 AND sl.source='server'
  ORDER BY sl.occurred_at DESC, sl.id DESC LIMIT 1
 ) latest ON latest.session_id=s.session_id AND latest.server_member_id=s.server_member_id
 WHERE latest.status='connected' AND s.source='server' AND s.status='connected'`

func statusSessionTiming(webhook string, activity presence.MemberPresence, connectedAt *time.Time, now time.Time) (string, string) {
	if webhook != "connected" {
		connectedAt = nil
		if activity.Playing && !activity.Connecting {
			connectedAt = activity.StartedAt
		}
	}
	if connectedAt == nil || connectedAt.After(now) {
		return "—", "—"
	}
	minutes := int64(now.Sub(*connectedAt) / time.Minute)
	return fmt.Sprintf("<t:%d:f>", connectedAt.Unix()), fmt.Sprintf("%dh%dm", minutes/60, minutes%60)
}

func (b *Bot) buildStatusEmbed(ctx context.Context, userID string) (*discordgo.MessageEmbed, error) {
	m, err := b.members.FindByDiscordUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find status member: %w", err)
	}
	var webhook string
	err = b.database.QueryRow(ctx, latestWebhookStatus, userID).Scan(&webhook)
	if err != nil {
		return nil, fmt.Errorf("read webhook status: %w", err)
	}
	var activity presence.MemberPresence
	snapshot, err := b.status.Snapshot(b.session)
	if err != nil {
		b.logger.Warn("status Discord snapshot unavailable", "error", err)
	} else {
		for _, entry := range snapshot {
			if entry.DiscordUserID == userID {
				activity = entry
				break
			}
		}
	}
	var connectedAt *time.Time
	if webhook == "connected" {
		if err := b.database.QueryRow(ctx, latestWebhookConnectedAt, userID).Scan(&connectedAt); err != nil {
			return nil, fmt.Errorf("read connected time: %w", err)
		}
	}
	now := time.Now().UTC()
	connectedTime, playtime := statusSessionTiming(webhook, activity, connectedAt, now)
	discordStatus := discordActivityStatus(activity)
	crStatus := "Disconnected"
	if webhook == "connected" {
		crStatus = "Connected"
	} else if webhook == "connecting" {
		crStatus = "Connecting"
	}
	currentStatus := connectionStatus(webhook, activity)
	color := 0xED4245
	if currentStatus == "Connecting" {
		color = 0xFEE75C
	} else if currentStatus == "Connected" {
		color = 0x57F287
	}
	return &discordgo.MessageEmbed{Title: "CR Roleplay Status", Color: color,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Name", Value: fmt.Sprintf("%s (<@%s>)", m.CharacterName, userID)},
			{Name: "Username", Value: statusIdentity(m.CFXName), Inline: true},
			{Name: "CID", Value: statusIdentity(m.CID), Inline: true},
			{Name: "Playtime", Value: playtime, Inline: true},
			{Name: "Current Status", Value: currentStatus},
			{Name: "CR Server", Value: crStatus, Inline: true},
			{Name: "Discord Activity", Value: discordStatus, Inline: true},
			{Name: "Connected Time", Value: connectedTime},
		}, Timestamp: now.Format(time.RFC3339)}, nil
}

func (b *Bot) handleStatus(session *discordgo.Session, message *discordgo.MessageCreate) error {
	id := message.Author.ID
	if len(message.Mentions) == 1 {
		id = message.Mentions[0].ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := b.buildStatusEmbed(ctx, id)
	if err != nil {
		return err
	}
	_, err = session.ChannelMessageSendEmbed(message.ChannelID, result)
	return err
}

func discordActivityStatus(activity presence.MemberPresence) string {
	if !activity.Playing {
		return "No Activity"
	}
	if activity.Connecting {
		return "Connecting"
	}
	return "Connected"
}

func statusIdentity(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "~", "\\~").Replace(value)
}
