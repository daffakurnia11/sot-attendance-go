package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
)

type stubCFXPlayers struct {
	players []dashboard.CFXPlayer
	err     error
}

func (s stubCFXPlayers) Players(context.Context) ([]dashboard.CFXPlayer, error) {
	return s.players, s.err
}

func TestRotatingStatusAlternatesDiscordAndCFX(t *testing.T) {
	t.Parallel()

	status, showCFX, ok := rotatingStatus("CR", 7, true, 11, true, false)
	if !ok || status != "7 CR players on Discord" || !showCFX {
		t.Fatalf("first status = %q, showCFX=%v, ok=%v", status, showCFX, ok)
	}
	status, showCFX, ok = rotatingStatus("CR", 7, true, 11, true, showCFX)
	if !ok || status != "11 CR players on CFX" || showCFX {
		t.Fatalf("second status = %q, showCFX=%v, ok=%v", status, showCFX, ok)
	}
}

func TestRotatingStatusUsesAvailableSource(t *testing.T) {
	t.Parallel()

	status, _, ok := rotatingStatus("CR", 7, true, 0, false, true)
	if !ok || status != "7 CR players on Discord" {
		t.Fatalf("Discord fallback = %q, ok=%v", status, ok)
	}
	status, _, ok = rotatingStatus("CR", 0, false, 11, true, false)
	if !ok || status != "11 CR players on CFX" {
		t.Fatalf("CFX fallback = %q, ok=%v", status, ok)
	}
	if _, _, ok := rotatingStatus("CR", 0, false, 0, false, false); ok {
		t.Fatal("unavailable sources returned a status")
	}
}

func TestRefreshCFXKeepsLastSuccessfulCount(t *testing.T) {
	t.Parallel()
	bot := &Bot{
		cfx:    stubCFXPlayers{players: []dashboard.CFXPlayer{{ID: 1}, {ID: 2}}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	bot.cfxCount.Store(-1)
	bot.refreshCFX(context.Background())
	if got := bot.cfxCount.Load(); got != 2 {
		t.Fatalf("CFX count = %d, want 2", got)
	}

	bot.cfx = stubCFXPlayers{err: errors.New("upstream unavailable")}
	bot.refreshCFX(context.Background())
	if got := bot.cfxCount.Load(); got != 2 {
		t.Fatalf("CFX count after failure = %d, want last successful 2", got)
	}
}

func TestShortServerName(t *testing.T) {
	t.Parallel()
	if got := shortServerName("CR Roleplay"); got != "CR" {
		t.Fatalf("shortServerName() = %q", got)
	}
}
