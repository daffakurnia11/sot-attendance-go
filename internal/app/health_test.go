package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestBotHealthTracksDiscordGatewayReadiness(t *testing.T) {
	t.Parallel()
	bot := &Bot{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	assertStatus := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		bot.HealthHandler().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("health status = %d, want %d", response.Code, want)
		}
	}

	assertStatus(http.StatusServiceUnavailable)
	bot.ready.Store(true)
	assertStatus(http.StatusOK)
	bot.onDisconnect(nil, &discordgo.Disconnect{})
	assertStatus(http.StatusServiceUnavailable)
	bot.onResumed(nil, &discordgo.Resumed{})
	assertStatus(http.StatusOK)
}
