package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/playerreport"
	"github.com/jackc/pgx/v5"
)

func (b *Bot) currentPlayerReport(ctx context.Context) (playerreport.Snapshot, error) {
	snapshot, err := b.playerDashboard.Get(ctx, 0)
	if err != nil {
		return playerreport.Snapshot{}, err
	}
	if b.status != nil && b.session != nil {
		presences, presenceErr := b.status.Snapshot(b.session)
		if presenceErr != nil {
			b.logger.Warn("report Discord presence unavailable", "error", presenceErr)
		} else {
			for index := range snapshot.DiscordPlayers {
				p := &snapshot.DiscordPlayers[index]
				for _, presence := range presences {
					if presence.DiscordUserID == p.DiscordUserID {
						p.DiscordStatus = presence.Status
						p.DiscordPlaying = presence.Playing
						p.DiscordConnecting = presence.Connecting
						break
					}
				}
			}
		}
	}
	return playerreport.Capture(time.Now(), snapshot.DiscordPlayers), nil
}
func (b *Bot) sendPlayerReport(ctx context.Context, snapshot playerreport.Snapshot, peak bool, channelID string) (string, error) {
	image, err := playerreport.Render(ctx, snapshot, b.location)
	if err != nil {
		return "", err
	}
	message, err := b.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: playerreport.Caption(snapshot, peak, b.location), Files: []*discordgo.File{{Name: "player-log.png", ContentType: "image/png", Reader: bytes.NewReader(image)}}, AllowedMentions: &discordgo.MessageAllowedMentions{}}, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return message.ID, nil
}
func (b *Bot) runPlayerReports(ctx context.Context) {
	if b.currentLogChannelID == "" {
		b.logger.Warn("player reports disabled: DISCORD_CURRENT_LOG_CHANNEL_ID unset")
		return
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 40*time.Second)
		if err := b.playerReportTick(work, time.Now()); err != nil {
			b.logger.Error("player peak report cycle", "error", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (b *Bot) playerReportTick(ctx context.Context, now time.Time) error {
	tx, err := b.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var claimed bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "player-report:"+b.guildID).Scan(&claimed); err != nil || !claimed {
		return err
	}
	date, active := playerreport.WindowDate(now, b.location)
	if active {
		snapshot, captureErr := b.currentPlayerReport(ctx)
		if captureErr != nil {
			return captureErr
		}
		// A poll that completed after midnight belongs to neither the previous peak nor today's window.
		capturedDate, inWindow := playerreport.WindowDate(snapshot.At, b.location)
		if inWindow && capturedDate == date {
			if err = savePlayerPeak(ctx, tx, b.guildID, date, snapshot); err != nil {
				return err
			}
		}
	}
	// Commit samples before rendering/network operations, so delivery failure cannot lose a new peak.
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return b.deliverPlayerPeak(ctx, date)
}
func (b *Bot) deliverPlayerPeak(ctx context.Context, today string) error {
	tx, err := b.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var date string
	var raw []byte
	var previousID *string
	err = tx.QueryRow(ctx, `SELECT to_char(report_date,'YYYY-MM-DD'),snapshot,delivered_message_id FROM player_peak_reports WHERE guild_id=$1 AND report_date<=$2::date AND member_count>posted_count AND next_attempt_at<=NOW() ORDER BY report_date FOR UPDATE SKIP LOCKED LIMIT 1`, b.guildID, today).Scan(&date, &raw, &previousID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var snapshot playerreport.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	// Delete only this evening's tracked automatic post, never manual reports.
	var sendErr error
	if previousID != nil {
		sendErr = b.session.ChannelMessageDelete(b.currentLogChannelID, *previousID, discordgo.WithContext(ctx))
		var restErr *discordgo.RESTError
		if errors.As(sendErr, &restErr) && restErr.Message != nil && restErr.Message.Code == discordgo.ErrCodeUnknownMessage {
			sendErr = nil
		}
	}
	var id string
	if sendErr == nil {
		id, sendErr = b.sendPlayerReport(ctx, snapshot, true, b.currentLogChannelID)
	}
	if sendErr != nil {
		// Keep the frozen peak; retry later without resampling it.
		_, err = tx.Exec(ctx, `UPDATE player_peak_reports SET next_attempt_at=NOW()+INTERVAL '1 minute' WHERE guild_id=$1 AND report_date=$2`, b.guildID, date)
		if err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return sendErr
	}
	// shortcut: Discord send and DB commit are not atomic; a crash in between can duplicate one report.
	if _, err = tx.Exec(ctx, `UPDATE player_peak_reports SET delivered_message_id=$3,posted_count=member_count WHERE guild_id=$1 AND report_date=$2`, b.guildID, date, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	b.logger.Info("player peak report posted", "date", date, "members", snapshot.ConnectedCount(), "message_id", id)
	return nil
}
func (b *Bot) handleCurrentLog(s *discordgo.Session, i *discordgo.Interaction) {
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral}}); err != nil {
		b.logger.Error("acknowledge currentlog", "error", err)
		return
	}
	reply := func(message string) { b.orderReply(s, i, message, nil, []discordgo.MessageComponent{}) }
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	uid, err := interactionUserID(i)
	if err != nil {
		reply("Guild member required.")
		return
	}
	member, err := b.members.FindByDiscordUserID(ctx, uid)
	if err != nil || !member.IsAdmin {
		reply("Administrator permission required.")
		return
	}
	if i.ChannelID == "" {
		reply("Channel required.")
		return
	}
	snapshot, err := b.currentPlayerReport(ctx)
	if err == nil {
		_, err = b.sendPlayerReport(ctx, snapshot, false, i.ChannelID)
	}
	if err != nil {
		b.logger.Error("manual player report", "user_id", uid, "error", err)
		reply("Could not post current log. Check renderer and channel permissions; retry shortly.")
		return
	}
	b.logger.Info("current player report posted", "user_id", uid, "channel_id", i.ChannelID, "members", snapshot.ConnectedCount())
	reply(fmt.Sprintf("Current log posted to <#%s> · %d connected members.", i.ChannelID, snapshot.ConnectedCount()))
}

func savePlayerPeak(ctx context.Context, tx pgx.Tx, guild, date string, snapshot playerreport.Snapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO player_peak_reports(guild_id,report_date,snapshot,member_count,captured_at) VALUES($1,$2,$3::jsonb,$4,$5)
 ON CONFLICT(guild_id,report_date) DO UPDATE SET snapshot=EXCLUDED.snapshot,member_count=EXCLUDED.member_count,captured_at=EXCLUDED.captured_at,next_attempt_at=NOW()
 WHERE EXCLUDED.member_count>player_peak_reports.member_count`, guild, date, string(data), snapshot.ConnectedCount(), snapshot.At)
	return err
}
