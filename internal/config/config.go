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
	Token                string
	GuildID              string
	ServerName           string
	PollInterval         time.Duration
	CommandPrefix        string
	PlayerLogChannelID   string
	BlacklistedUserIDs   []string
	PlayerChatChannelID  string
	PlayerRecapChannelID string
	AttendanceStartTime  time.Duration
	AttendanceEndTime    time.Duration
	AttendancePlaytime   time.Duration
	DatabaseURL          string
}

func Load() (Config, error) {
	return FromValues(
		os.Getenv("DISCORD_BOT_TOKEN"),
		os.Getenv("DISCORD_GUILD_ID"),
		os.Getenv("FIVEM_SERVER_NAME"),
		os.Getenv("DISCORD_POLL_INTERVAL"),
		os.Getenv("DISCORD_COMMAND_PREFIX"),
		os.Getenv("DISCORD_PLAYER_LOG_CHANNEL_ID"),
		os.Getenv("BLACKLISTED_USERS"),
		os.Getenv("DISCORD_PLAYER_CHAT_CHANNEL_ID"),
		os.Getenv("DISCORD_PLAYER_RECAP_CHANNEL_ID"),
		os.Getenv("ATTENDANCE_START_TIME"),
		os.Getenv("ATTENDANCE_END_TIME"),
		os.Getenv("ATTENDANCE_PLAYTIME"),
		os.Getenv("DATABASE_URL"),
	)
}

func FromValues(token, guildID, serverName, pollInterval, commandPrefix, playerLogChannelID, blacklistedUsers, playerChatChannelID, playerRecapChannelID, attendanceStartTime, attendanceEndTime, attendancePlaytime, databaseURL string) (Config, error) {
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

	playerChatChannelID = strings.TrimSpace(playerChatChannelID)
	if err := validateDiscordID(playerChatChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_PLAYER_CHAT_CHANNEL_ID: %w", err)
	}
	playerRecapChannelID = strings.TrimSpace(playerRecapChannelID)
	if err := validateDiscordID(playerRecapChannelID); err != nil {
		return Config{}, fmt.Errorf("DISCORD_PLAYER_RECAP_CHANNEL_ID: %w", err)
	}

	startTime, err := parseClockTime(attendanceStartTime)
	if err != nil {
		return Config{}, fmt.Errorf("ATTENDANCE_START_TIME: %w", err)
	}
	endTime, err := parseClockTime(attendanceEndTime)
	if err != nil {
		return Config{}, fmt.Errorf("ATTENDANCE_END_TIME: %w", err)
	}
	if startTime == endTime {
		return Config{}, errors.New("ATTENDANCE_START_TIME and ATTENDANCE_END_TIME must differ")
	}
	requiredPlaytime, err := time.ParseDuration(strings.TrimSpace(attendancePlaytime))
	if err != nil || requiredPlaytime <= 0 {
		return Config{}, errors.New("ATTENDANCE_PLAYTIME must be a positive Go duration such as 90m or 1h30m")
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	return Config{
		Token:                token,
		GuildID:              guildID,
		ServerName:           serverName,
		PollInterval:         time.Duration(pollMilliseconds) * time.Millisecond,
		CommandPrefix:        commandPrefix,
		PlayerLogChannelID:   playerLogChannelID,
		BlacklistedUserIDs:   blacklistedUserIDs,
		PlayerChatChannelID:  playerChatChannelID,
		PlayerRecapChannelID: playerRecapChannelID,
		AttendanceStartTime:  startTime,
		AttendanceEndTime:    endTime,
		AttendancePlaytime:   requiredPlaytime,
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

func parseClockTime(value string) (time.Duration, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, errors.New("must use HH:MM 24-hour format")
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
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
