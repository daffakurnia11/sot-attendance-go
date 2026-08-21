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
	PlayerLogChannelID   string
	BlacklistedUserIDs   []string
	PlayerChatChannelID  string
	PlayerRecapChannelID string
	DatabaseURL          string
}

func Load() (Config, error) {
	config, err := FromValues(
		os.Getenv("DISCORD_BOT_TOKEN"),
		os.Getenv("DISCORD_GUILD_ID"),
		os.Getenv("FIVEM_SERVER_NAME"),
		os.Getenv("DISCORD_POLL_INTERVAL"),
		os.Getenv("DISCORD_COMMAND_PREFIX"),
		os.Getenv("DISCORD_PLAYER_LOG_CHANNEL_ID"),
		os.Getenv("BLACKLISTED_USERS"),
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
	return withStatusPolling(config, os.Getenv("FIVEM_SERVER_CFX_ID"), os.Getenv("FIVEM_PLAYER_ID"), os.Getenv("FIVEM_SERVER_CFX_POLL_INTERVAL"), os.Getenv("DISCORD_POLL_STATUS"))
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

func FromValues(token, guildID, serverName, pollInterval, commandPrefix, playerLogChannelID, blacklistedUsers, playerChatChannelID, playerRecapChannelID, databaseURL, appEnv, discordRoleID, discordAdminIDs string) (Config, error) {
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

	playerLogChannelID = strings.TrimSpace(playerLogChannelID)
	if playerLogChannelID == "" {
		return Config{}, errors.New("DISCORD_PLAYER_LOG_CHANNEL_ID is required")
	}
	for _, char := range playerLogChannelID {
		if char < '0' || char > '9' {
			return Config{}, errors.New("DISCORD_PLAYER_LOG_CHANNEL_ID must contain digits only")
		}
	}

	blacklistedUserIDs, err := parseDiscordIDs(blacklistedUsers)
	if err != nil {
		return Config{}, fmt.Errorf("BLACKLISTED_USERS: %w", err)
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
		PlayerLogChannelID:   playerLogChannelID,
		BlacklistedUserIDs:   blacklistedUserIDs,
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
