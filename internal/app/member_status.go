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
	discordStatus := discordActivityStatus(activity)
	crStatus := "Disconnected"
	if webhook == "connected" {
		crStatus = "Connected"
	} else if webhook == "connecting" {
		crStatus = "Connecting"
	}
	return &discordgo.MessageEmbed{Title: "CR Roleplay Status", Color: 0xF2B63D,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Name", Value: fmt.Sprintf("%s (<@%s>)", m.CharacterName, userID)},
			{Name: "Username", Value: statusIdentity(m.CFXName), Inline: true},
			{Name: "CID", Value: statusIdentity(m.CID), Inline: true},
			{Name: "\u200b", Value: "\u200b", Inline: true},
			{Name: "Current Status", Value: connectionStatus(webhook, activity)},
			{Name: "CR Server", Value: crStatus, Inline: true},
			{Name: "Discord Activity", Value: discordStatus, Inline: true},
		}, Timestamp: time.Now().UTC().Format(time.RFC3339)}, nil
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
