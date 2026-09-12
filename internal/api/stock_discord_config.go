package api

import (
	"fmt"
	"os"
	"strings"
)

type StockDiscordConfig struct {
	Token    string
	Channels StockDiscordChannels
}

func LoadStockDiscordConfig() (StockDiscordConfig, error) {
	return StockDiscordConfigFromValues(
		os.Getenv("DISCORD_BOT_TOKEN"),
		os.Getenv("STASH_PUBLIC_CHANNEL_ID"),
		os.Getenv("STASH_BOSS_CHANNEL_ID"),
	)
}

func StockDiscordConfigFromValues(token, publicChannelID, bossChannelID string) (StockDiscordConfig, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return StockDiscordConfig{}, fmt.Errorf("DISCORD_BOT_TOKEN is required")
	}
	values := []struct{ name, value string }{
		{"STASH_PUBLIC_CHANNEL_ID", publicChannelID},
		{"STASH_BOSS_CHANNEL_ID", bossChannelID},
	}
	for index := range values {
		values[index].value = strings.TrimSpace(values[index].value)
		if !isDiscordID(values[index].value) {
			return StockDiscordConfig{}, fmt.Errorf("%s must contain digits only", values[index].name)
		}
	}
	return StockDiscordConfig{Token: token, Channels: StockDiscordChannels{Public: values[0].value, Boss: values[1].value}}, nil
}
