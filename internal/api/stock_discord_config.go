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
		os.Getenv("STASH_BOSS_DP_CHANNEL_ID"),
		os.Getenv("STASH_BOSS_WD_CHANNEL_ID"),
		os.Getenv("STASH_PUBLIC_DP_CHANNEL_ID"),
		os.Getenv("STASH_PUBLIC_WD_CHANNEL_ID"),
	)
}

func StockDiscordConfigFromValues(token, bossDeposit, bossWithdraw, publicDeposit, publicWithdraw string) (StockDiscordConfig, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return StockDiscordConfig{}, fmt.Errorf("DISCORD_BOT_TOKEN is required")
	}
	values := []struct{ name, value string }{
		{"STASH_BOSS_DP_CHANNEL_ID", bossDeposit},
		{"STASH_BOSS_WD_CHANNEL_ID", bossWithdraw},
		{"STASH_PUBLIC_DP_CHANNEL_ID", publicDeposit},
		{"STASH_PUBLIC_WD_CHANNEL_ID", publicWithdraw},
	}
	for index := range values {
		values[index].value = strings.TrimSpace(values[index].value)
		if !isDiscordID(values[index].value) {
			return StockDiscordConfig{}, fmt.Errorf("%s must contain digits only", values[index].name)
		}
	}
	return StockDiscordConfig{Token: token, Channels: StockDiscordChannels{
		BossDeposit: values[0].value, BossWithdraw: values[1].value,
		PublicDeposit: values[2].value, PublicWithdraw: values[3].value,
	}}, nil
}
