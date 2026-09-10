package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv               string
	Token                string
	GuildID              string
	DiscordRoleID        string
	DiscordMemberRoleID  string
	DiscordAdminRoleIDs  []string
	ServerName           string
	PollInterval         time.Duration
	CFXServerID          string
	CFXPlayerID          string
	CFXPollInterval      time.Duration
	StatusPollInterval   time.Duration
	CommandPrefix        string
	ServerLogChannelID   string
	PlayerChatChannelID  string
	PlayerRecapChannelID string
	OfficeMoneyChannelID string
	DirtyMoneyChannelID  string
	StashChannels        StashChannels
	DatabaseURL          string
}

// StashChannels names the four safebox channels. Each one fixes both the
// safebox and the action, so a stash command carries neither: posting in the
// public withdraw channel can only withdraw from the public safebox. The same
// four channels already receive the crafting calculator's stock embeds, so a
// channel reads as one ledger regardless of which side wrote the row.
type StashChannels struct {
	BossDeposit    string
	BossWithdraw   string
	PublicDeposit  string
	PublicWithdraw string
}

// Announces reports whether this process may act on Discord and on shared
// rows: post announcements, sync the roster, set the bot status.
//
// Only production may. A local run points at the same token and, usually, the
// same database as the deployed bot, so anything it does is done twice - every
// announcement posted again, and the roster sync fighting the deployed process
// over the same rows. A local bot therefore reads and serves and nothing else.
func (c Config) Announces() bool { return c.AppEnv == "production" }

func Load() (Config, error) {
	config, err := FromValues(
		os.Getenv("DISCORD_BOT_TOKEN"),
		os.Getenv("DISCORD_GUILD_ID"),
		os.Getenv("FIVEM_SERVER_NAME"),
		os.Getenv("DISCORD_POLL_INTERVAL"),
		os.Getenv("DISCORD_COMMAND_PREFIX"),
		os.Getenv("DISCORD_PLAYER_CHAT_CHANNEL_ID"),
		os.Getenv("DISCORD_PLAYER_RECAP_CHANNEL_ID"),
		os.Getenv("DATABASE_URL"),
		os.Getenv("APP_ENV"),
		os.Getenv("DISCORD_ROLE_ID"),
		os.Getenv("DISCORD_ADMIN_IDS"),
	)
	if err != nil {
		return Config{}, err
	}
	config, err = withMoneyChannels(config, os.Getenv("DISCORD_OFFICE_MONEY_CHANNEL_ID"), os.Getenv("DISCORD_DIRTY_MONEY_CHANNEL_ID"))
	if err != nil {
		return Config{}, err
	}
	config, err = withStashChannels(
		config,
		os.Getenv("STASH_BOSS_DP_CHANNEL_ID"),
		os.Getenv("STASH_BOSS_WD_CHANNEL_ID"),
		os.Getenv("STASH_PUBLIC_DP_CHANNEL_ID"),
		os.Getenv("STASH_PUBLIC_WD_CHANNEL_ID"),
	)
	if err != nil {
		return Config{}, err
	}
	config, err = withServerLogChannel(config, os.Getenv("DISCORD_SERVER_LOG_CHANNEL_ID"))
	if err != nil {
		return Config{}, err
	}
	return withStatusPolling(config, os.Getenv("FIVEM_SERVER_CFX_ID"), os.Getenv("FIVEM_PLAYER_ID"), os.Getenv("FIVEM_SERVER_CFX_POLL_INTERVAL"), os.Getenv("DISCORD_POLL_STATUS"))
}

func withMoneyChannels(config Config, officeChannelID, dirtyChannelID string) (Config, error) {
	officeChannelID = strings.TrimSpace(officeChannelID)
	if err := validateDiscordID(officeChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_OFFICE_MONEY_CHANNEL_ID: %w", err)
	}
	dirtyChannelID = strings.TrimSpace(dirtyChannelID)
	if err := validateDiscordID(dirtyChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_DIRTY_MONEY_CHANNEL_ID: %w", err)
	}
	if officeChannelID == dirtyChannelID {
		return Config{}, errors.New("DISCORD_OFFICE_MONEY_CHANNEL_ID and DISCORD_DIRTY_MONEY_CHANNEL_ID must be different")
	}
	config.OfficeMoneyChannelID = officeChannelID
	config.DirtyMoneyChannelID = dirtyChannelID
	return config, nil
}

// withStashChannels validates the four safebox channels and refuses to let any
// two share an ID. A shared ID would make one channel mean two things - the
// safebox and the action are read from the channel and nothing else - so the
// bot could not tell a public deposit from a boss withdrawal.
func withStashChannels(config Config, bossDeposit, bossWithdraw, publicDeposit, publicWithdraw string) (Config, error) {
	values := []struct{ name, value string }{
		{"STASH_BOSS_DP_CHANNEL_ID", bossDeposit},
		{"STASH_BOSS_WD_CHANNEL_ID", bossWithdraw},
		{"STASH_PUBLIC_DP_CHANNEL_ID", publicDeposit},
		{"STASH_PUBLIC_WD_CHANNEL_ID", publicWithdraw},
	}
	seen := make(map[string]string, len(values))
	for index := range values {
		values[index].value = strings.TrimSpace(values[index].value)
		if err := validateDiscordID(values[index].value); err != nil {
			return Config{}, fmt.Errorf("%s: %w", values[index].name, err)
		}
		if previous, duplicate := seen[values[index].value]; duplicate {
			return Config{}, fmt.Errorf("%s and %s must be different", previous, values[index].name)
		}
		seen[values[index].value] = values[index].name
	}
	config.StashChannels = StashChannels{
		BossDeposit: values[0].value, BossWithdraw: values[1].value,
		PublicDeposit: values[2].value, PublicWithdraw: values[3].value,
	}
	return config, nil
}

// withServerLogChannel validates the channel the FiveM server log posts to.
//
// It used to also refuse to share a channel with the Discord activity log. That
// log is gone - the game server reports the same events over the webhook - so
// there is no second feed left to collide with.
func withServerLogChannel(config Config, serverLogChannelID string) (Config, error) {
	serverLogChannelID = strings.TrimSpace(serverLogChannelID)
	if err := validateDiscordID(serverLogChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_SERVER_LOG_CHANNEL_ID: %w", err)
	}
	config.ServerLogChannelID = serverLogChannelID
	return config, nil
}

func withStatusPolling(config Config, cfxServerID, cfxPlayerID, cfxPollInterval, statusPollInterval string) (Config, error) {
	cfxServerID = strings.TrimSpace(cfxServerID)
	if cfxServerID == "" {
		return Config{}, errors.New("FIVEM_SERVER_CFX_ID is required")
	}
	for _, character := range cfxServerID {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')) {
			return Config{}, errors.New("FIVEM_SERVER_CFX_ID must contain letters and digits only")
		}
	}
	cfxPlayerID = strings.TrimSpace(cfxPlayerID)
	if cfxPlayerID == "" {
		return Config{}, errors.New("FIVEM_PLAYER_ID is required")
	}
	cfxPoll, err := parseMilliseconds("FIVEM_SERVER_CFX_POLL_INTERVAL", cfxPollInterval)
	if err != nil {
		return Config{}, err
	}
	statusPoll, err := parseMilliseconds("DISCORD_POLL_STATUS", statusPollInterval)
	if err != nil {
		return Config{}, err
	}
	config.CFXServerID = cfxServerID
	config.CFXPlayerID = cfxPlayerID
	config.CFXPollInterval = cfxPoll
	config.StatusPollInterval = statusPoll
	return config, nil
}

func parseMilliseconds(name, value string) (time.Duration, error) {
	milliseconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || milliseconds <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer in milliseconds", name)
	}
	if milliseconds > int64((1<<63-1)/time.Millisecond) {
		return 0, fmt.Errorf("%s is too large", name)
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func FromValues(token, guildID, serverName, pollInterval, commandPrefix, playerChatChannelID, playerRecapChannelID, databaseURL, appEnv, discordRoleID, discordAdminIDs string) (Config, error) {
	appEnv = strings.ToLower(strings.TrimSpace(appEnv))
	if appEnv != "local" && appEnv != "production" {
		return Config{}, errors.New("APP_ENV must be local or production")
	}
	discordRoleID = strings.TrimSpace(discordRoleID)
	discordMemberRoleID := discordRoleID
	if discordMemberRoleID != "" {
		if err := validateDiscordID(discordMemberRoleID); err != nil {
			return Config{}, fmt.Errorf("DISCORD_ROLE_ID: %w", err)
		}
	}
	if appEnv == "production" {
		if err := validateDiscordID(discordRoleID); err != nil {
			return Config{}, fmt.Errorf("DISCORD_ROLE_ID: %w", err)
		}
	} else {
		discordRoleID = ""
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return Config{}, errors.New("DISCORD_BOT_TOKEN is required")
	}

	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return Config{}, errors.New("DISCORD_GUILD_ID is required")
	}
	for _, char := range guildID {
		if char < '0' || char > '9' {
			return Config{}, errors.New("DISCORD_GUILD_ID must contain digits only")
		}
	}

	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return Config{}, errors.New("FIVEM_SERVER_NAME is required")
	}

	pollMilliseconds, err := strconv.ParseInt(strings.TrimSpace(pollInterval), 10, 64)
	if err != nil || pollMilliseconds <= 0 {
		return Config{}, errors.New("DISCORD_POLL_INTERVAL must be a positive integer in milliseconds")
	}
	if pollMilliseconds > int64((1<<63-1)/time.Millisecond) {
		return Config{}, errors.New("DISCORD_POLL_INTERVAL is too large")
	}

	commandPrefix = strings.TrimSpace(commandPrefix)
	if commandPrefix == "" {
		commandPrefix = "!"
	}
	if strings.ContainsAny(commandPrefix, " \t\r\n") {
		return Config{}, errors.New("DISCORD_COMMAND_PREFIX cannot contain whitespace")
	}

	discordAdminRoleIDs, err := parseDiscordIDs(discordAdminIDs)
	if err != nil {
		return Config{}, fmt.Errorf("DISCORD_ADMIN_IDS: %w", err)
	}

	playerChatChannelID = strings.TrimSpace(playerChatChannelID)
	if err := validateDiscordID(playerChatChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_PLAYER_CHAT_CHANNEL_ID: %w", err)
	}
	playerRecapChannelID = strings.TrimSpace(playerRecapChannelID)
	if err := validateDiscordID(playerRecapChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_PLAYER_RECAP_CHANNEL_ID: %w", err)
	}

	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	return Config{
		AppEnv:               appEnv,
		Token:                token,
		GuildID:              guildID,
		DiscordRoleID:        discordRoleID,
		DiscordMemberRoleID:  discordMemberRoleID,
		DiscordAdminRoleIDs:  discordAdminRoleIDs,
		ServerName:           serverName,
		PollInterval:         time.Duration(pollMilliseconds) * time.Millisecond,
		CommandPrefix:        commandPrefix,
		PlayerChatChannelID:  playerChatChannelID,
		PlayerRecapChannelID: playerRecapChannelID,
		DatabaseURL:          databaseURL,
	}, nil
}

func validateDiscordID(id string) error {
	if id == "" {
		return errors.New("is required")
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			return errors.New("must contain digits only")
		}
	}
	return nil
}

func parseDiscordIDs(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, rawID := range strings.Split(value, ",") {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, errors.New("contains an empty Discord ID")
		}
		for _, char := range id {
			if char < '0' || char > '9' {
				return nil, fmt.Errorf("Discord ID %q must contain digits only", id)
			}
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}
