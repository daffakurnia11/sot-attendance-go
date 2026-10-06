package app

import (
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"testing"
)

func TestShortServerName(t *testing.T) {
	t.Parallel()
	if got := shortServerName("CR Roleplay"); got != "CR" {
		t.Fatalf("shortServerName() = %q", got)
	}
}

func TestWatchingPlayerStatus(t *testing.T) {
	players := []dashboard.Player{
		{Status: "connected", DiscordStatus: "unknown"},
		{Status: "connected", DiscordPlaying: false},
		{Status: "connecting"},
		{Status: "offline", DiscordPlaying: true},
	}
	if got := watchingPlayerStatus(players); got != "Watching 3 CR Players" {
		t.Fatalf("activity = %q", got)
	}
	if got := watchingPlayerStatus(nil); got != "Watching 0 CR Players" {
		t.Fatalf("empty activity = %q", got)
	}
}
