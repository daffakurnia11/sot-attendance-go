package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	commandrecap "github.com/daffakurniawan/sot-discord-bot/internal/command/recap"
)

func slashCommands() []*discordgo.ApplicationCommand {
	guildContexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	return []*discordgo.ApplicationCommand{
		{
			Name: commandrecap.CheckCommand, Description: "Check attendance and playtime", Contexts: &guildContexts,
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionUser, Name: "member",
				Description: "Member to check; defaults to you", Required: false,
			}},
		},
		{Name: commandrecap.Command, Description: "Show the current attendance recap", Contexts: &guildContexts},
	}
}

func (b *Bot) registerSlashCommands(session *discordgo.Session, applicationID string) {
	commands, err := session.ApplicationCommandBulkOverwrite(applicationID, b.guildID, slashCommands())
	if err != nil {
		b.logger.Error("register guild slash commands", "guild_id", b.guildID, "error", err)
		return
	}
	b.logger.Info("guild slash commands registered", "guild_id", b.guildID, "commands", len(commands))
}

func (b *Bot) onInteractionCreate(session *discordgo.Session, event *discordgo.InteractionCreate) {
	if event == nil || event.Interaction == nil || event.Type != discordgo.InteractionApplicationCommand || event.GuildID != b.guildID {
		return
	}
	commandName := event.ApplicationCommandData().Name
	if !isSlashCommand(commandName) {
		return
	}

	if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		b.logger.Error("defer slash command response", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "error", err)
		return
	}

	response, userID, err := b.slashCommandResponse(event.Interaction, commandName)
	if err != nil {
		b.logger.Error("handle slash command", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", userID, "error", err)
		content := "Could not run that command. Check your permissions or try again shortly."
		response = &discordgo.WebhookEdit{Content: &content}
	}
	if _, editErr := session.InteractionResponseEdit(event.Interaction, response); editErr != nil {
		b.logger.Error("edit slash command response", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", userID, "error", editErr)
		return
	}
	if err == nil {
		b.logger.Info("slash command sent", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", userID)
	}
}

func isSlashCommand(name string) bool {
	switch name {
	case commandrecap.CheckCommand, commandrecap.Command:
		return true
	default:
		return false
	}
}

func (b *Bot) slashCommandResponse(interaction *discordgo.Interaction, commandName string) (*discordgo.WebhookEdit, string, error) {
	if interaction.Member == nil || interaction.Member.User == nil {
		return nil, "", errors.New("guild member is required")
	}
	userID := interaction.Member.User.ID
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch commandName {
	case commandrecap.CheckCommand:
		targetUserID, err := checkSlashTargetUserID(interaction, userID)
		if err != nil {
			return nil, userID, err
		}
		embed, _, _, err := b.buildCheckEmbed(ctx, targetUserID, now)
		return embedEdit(embed), userID, err
	case commandrecap.Command:
		embed, _, _, err := b.buildRecapEmbed(ctx, now)
		return embedEdit(embed), userID, err
	default:
		return nil, userID, fmt.Errorf("unsupported slash command %q", commandName)
	}
}

func checkSlashTargetUserID(interaction *discordgo.Interaction, fallbackUserID string) (string, error) {
	option := interaction.ApplicationCommandData().GetOption("member")
	if option == nil {
		return fallbackUserID, nil
	}
	if option.Type != discordgo.ApplicationCommandOptionUser {
		return "", errors.New("member option must be a user")
	}
	targetUserID, ok := option.Value.(string)
	if !ok || targetUserID == "" {
		return "", errors.New("member option has invalid user ID")
	}
	return targetUserID, nil
}

func embedEdit(messageEmbed *discordgo.MessageEmbed) *discordgo.WebhookEdit {
	if messageEmbed == nil {
		return nil
	}
	embeds := []*discordgo.MessageEmbed{messageEmbed}
	return &discordgo.WebhookEdit{Embeds: &embeds}
}

func (b *Bot) buildRecapEmbed(ctx context.Context, now time.Time) (*discordgo.MessageEmbed, int, time.Time, error) {
	attendanceConfig, err := b.settings.LoadAttendance(ctx)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("reload attendance settings: %w", err)
	}
	attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, attendanceConfig.StartTime, attendanceConfig.EndTime, b.location)
	recaps, err := b.members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	return commandrecap.Embed(recaps, attendanceStart, now, attendanceConfig.PlaytimeThreshold), len(recaps), attendanceStart, nil
}

func (b *Bot) buildCheckEmbed(ctx context.Context, userID string, now time.Time) (*discordgo.MessageEmbed, int, time.Time, error) {
	attendanceConfig, err := b.settings.LoadAttendance(ctx)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("reload attendance settings: %w", err)
	}
	attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, attendanceConfig.StartTime, attendanceConfig.EndTime, b.location)
	currentMember, err := b.members.FindByUserID(ctx, userID)
	if err != nil {
		return nil, 0, time.Time{}, fmt.Errorf("find command member: %w", err)
	}
	recaps, err := b.members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	return commandrecap.CheckEmbed(currentMember, recaps, attendanceStart, now, attendanceConfig.PlaytimeThreshold), len(recaps), attendanceStart, nil
}
