package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	commandcrafting "github.com/daffakurniawan/sot-discord-bot/internal/command/crafting"
	commandmoney "github.com/daffakurniawan/sot-discord-bot/internal/command/money"
	commandrecap "github.com/daffakurniawan/sot-discord-bot/internal/command/recap"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
)

var errMoneyAdminRequired = errors.New("money command requires administrator permission")

type moneyChannelError struct {
	officeChannelID string
	dirtyChannelID  string
}

func (err moneyChannelError) Error() string {
	return "money command used outside configured money channels"
}

func slashCommands() []*discordgo.ApplicationCommand {
	guildContexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	return []*discordgo.ApplicationCommand{
		{Name: commandcrafting.Command, Description: "Build a multi-product crafting plan", Contexts: &guildContexts},
		{
			Name: commandrecap.CheckCommand, Description: "Check attendance and playtime", Contexts: &guildContexts,
			Options: []*discordgo.ApplicationCommandOption{{
				Type: discordgo.ApplicationCommandOptionUser, Name: "member",
				Description: "Member to check; defaults to you", Required: false,
			}},
		},
		{Name: commandrecap.Command, Description: "Show the current attendance recap", Contexts: &guildContexts},
		moneySlashCommand(guildContexts),
	}
}

func moneySlashCommand(guildContexts []discordgo.InteractionContextType) *discordgo.ApplicationCommand {
	mutation := func(name, description string) *discordgo.ApplicationCommandOption {
		minimum := 1.0
		return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionSubCommand, Name: name, Description: description, Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionInteger, Name: "amount", Description: "Positive whole-number amount", Required: true, MinValue: &minimum},
			{Type: discordgo.ApplicationCommandOptionString, Name: "reason", Description: "Transaction reason", Required: true, MaxLength: 500},
		}}
	}
	return &discordgo.ApplicationCommand{Name: commandmoney.Command, Description: "View or change current channel money", Contexts: &guildContexts, Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "balance", Description: "View current channel balance"},
		mutation(moneydomain.ActionDeposit, "Deposit money"),
		mutation(moneydomain.ActionWithdraw, "Withdraw money"),
	}}
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
	if event == nil || event.Interaction == nil || event.GuildID != b.guildID {
		return
	}
	if event.Type == discordgo.InteractionMessageComponent || event.Type == discordgo.InteractionModalSubmit {
		if isCraftInteraction(event.Interaction) {
			b.handleCraftInteraction(session, event.Interaction)
		}
		return
	}
	if event.Type != discordgo.InteractionApplicationCommand {
		return
	}
	commandName := event.ApplicationCommandData().Name
	if !isSlashCommand(commandName) {
		return
	}
	if commandName == commandcrafting.Command {
		b.handleCraftSlashStart(session, event.Interaction)
		return
	}

	if err := session.InteractionRespond(event.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource}); err != nil {
		b.logger.Error("defer slash command response", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "error", err)
		return
	}

	response, userID, err := b.slashCommandResponse(event.Interaction, commandName)
	if err != nil {
		b.logger.Error("handle slash command", "command", commandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", userID, "error", err)
		content := slashUserError(err)
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
	case commandrecap.CheckCommand, commandrecap.Command, commandcrafting.Command, commandmoney.Command:
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
	case commandmoney.Command:
		request, err := parseMoneySlashRequest(interaction)
		if err != nil {
			return nil, userID, err
		}
		account, validChannel := b.moneyAccountForChannel(interaction.ChannelID)
		if !validChannel {
			return nil, userID, moneyChannelError{officeChannelID: b.officeMoneyChannelID, dirtyChannelID: b.dirtyMoneyChannelID}
		}
		request.Account = account
		currentMember, err := b.members.FindByUserID(ctx, userID)
		if err != nil {
			return nil, userID, fmt.Errorf("find money command member: %w", err)
		}
		if request.Action == "balance" {
			balance, err := b.money.Balance(ctx, request.Account)
			return embedEdit(commandmoney.BalanceEmbed(request.Account, balance)), userID, err
		}
		if !currentMember.IsAdmin {
			return nil, userID, errMoneyAdminRequired
		}
		transaction, err := b.money.Transact(ctx, moneydomain.Transaction{Account: request.Account, Action: request.Action, Amount: request.Amount, Reason: request.Reason, ActorMemberID: currentMember.ID})
		if err != nil {
			return nil, userID, err
		}
		b.logger.Info("money transaction applied", "guild_id", interaction.GuildID, "channel_id", interaction.ChannelID, "user_id", userID, "member_id", currentMember.ID, "transaction_id", transaction.ID, "account", transaction.Account, "action", transaction.Action, "amount", transaction.Amount)
		return embedEdit(commandmoney.TransactionEmbed(transaction, currentMember.CharacterName)), userID, nil
	default:
		return nil, userID, fmt.Errorf("unsupported slash command %q", commandName)
	}
}

func parseMoneySlashRequest(interaction *discordgo.Interaction) (commandmoney.Request, error) {
	options := interaction.ApplicationCommandData().Options
	if len(options) != 1 || options[0].Type != discordgo.ApplicationCommandOptionSubCommand {
		return commandmoney.Request{}, commandmoney.ErrInvalidSyntax
	}
	subcommand := options[0]
	request := commandmoney.Request{Action: subcommand.Name}
	if request.Action == "balance" {
		return request, nil
	}
	amountOption, reasonOption := subcommand.GetOption("amount"), subcommand.GetOption("reason")
	if amountOption == nil || reasonOption == nil || amountOption.Type != discordgo.ApplicationCommandOptionInteger {
		return commandmoney.Request{}, commandmoney.ErrInvalidSyntax
	}
	reason, ok := reasonOption.Value.(string)
	if !ok {
		return commandmoney.Request{}, commandmoney.ErrInvalidSyntax
	}
	request.Amount, request.Reason = amountOption.IntValue(), reason
	if request.Amount <= 0 || request.Reason == "" || len(request.Reason) > 500 || (request.Action != moneydomain.ActionDeposit && request.Action != moneydomain.ActionWithdraw) {
		return commandmoney.Request{}, commandmoney.ErrInvalidSyntax
	}
	return request, nil
}

func slashUserError(err error) string {
	var channelError moneyChannelError
	switch {
	case errors.As(err, &channelError):
		return "Money commands can only be used in <#" + channelError.officeChannelID + "> or <#" + channelError.dirtyChannelID + ">."
	case errors.Is(err, errMoneyAdminRequired):
		return "Administrator permission is required to change money balances."
	case errors.Is(err, moneydomain.ErrInsufficientFunds):
		return "Insufficient balance for that withdrawal."
	case errors.Is(err, commandmoney.ErrInvalidSyntax), errors.Is(err, moneydomain.ErrInvalidAmount), errors.Is(err, moneydomain.ErrInvalidReason):
		return "Invalid money request. Check the amount and reason."
	default:
		return "Could not run that command. Check your permissions or try again shortly."
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
