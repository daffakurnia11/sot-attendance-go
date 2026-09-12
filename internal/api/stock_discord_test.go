package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

func TestStockDiscordConfigValidatesChannels(t *testing.T) {
	config, err := StockDiscordConfigFromValues(" token ", "33", "11")
	if err != nil || config.Token != "token" || config.Channels.Public != "33" || config.Channels.Boss != "11" {
		t.Fatalf("config = %#v, error = %v", config, err)
	}
	if _, err := StockDiscordConfigFromValues("token", "invalid", "11"); err == nil || !strings.Contains(err.Error(), "STASH_PUBLIC_CHANNEL_ID") {
		t.Fatalf("invalid channel error = %v", err)
	}
}

func TestDiscordStockNotifierRoutesEachMovementGroup(t *testing.T) {
	paths := make([]string, 0, 4)
	titles := make([]string, 0, 4)
	descriptions := make([]string, 0, 4)
	fields := make([][]struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}, 0, 4)
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		if request.Header.Get("Authorization") != "Bot secret" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload struct {
			Embeds []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Fields      []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"fields"`
			} `json:"embeds"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		paths = append(paths, request.URL.Path)
		titles = append(titles, payload.Embeds[0].Title)
		descriptions = append(descriptions, payload.Embeds[0].Description)
		fields = append(fields, payload.Embeds[0].Fields)
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	notifier := NewDiscordStockNotifier(client, "secret", StockDiscordChannels{Public: "33", Boss: "11"})
	notifier.baseURL = "https://discord.test"
	err := notifier.Notify(context.Background(), "123", []stock.Movement{
		{Safebox: "public", ItemKey: "iron", Action: stock.ActionWithdraw, Quantity: 80},
		{Safebox: "boss", ItemKey: "iron", Action: stock.ActionWithdraw, Quantity: 20},
		{Safebox: "boss", ItemKey: "vector", Action: stock.ActionDeposit, Quantity: 5},
		{Safebox: "public", ItemKey: "mp9", Action: stock.ActionDeposit, Quantity: 2},
	}, map[string]string{"iron": "Iron", "vector": "Vector", "mp9": "MP9"})
	if err != nil {
		t.Fatal(err)
	}
	// Both of a safebox's actions post to its one channel; the title separates them.
	wantPaths := []string{"/channels/11/messages", "/channels/11/messages", "/channels/33/messages", "/channels/33/messages"}
	wantTitles := []string{"Deposit · Boss Stash", "Withdraw · Boss Stash", "Deposit · Public Stash", "Withdraw · Public Stash"}
	if !reflect.DeepEqual(paths, wantPaths) || !reflect.DeepEqual(titles, wantTitles) {
		t.Fatalf("paths = %v, titles = %v", paths, titles)
	}
	wantDescriptions := []string{
		"Purpose: Crafting Products · <@123>",
		"Purpose: Crafting Materials · <@123>",
		"Purpose: Crafting Products · <@123>",
		"Purpose: Crafting Materials · <@123>",
	}
	if !reflect.DeepEqual(descriptions, wantDescriptions) {
		t.Fatalf("descriptions = %v", descriptions)
	}
	for index := range fields {
		if len(fields[index]) != 1 || fields[index][0].Name != "Items" {
			t.Fatalf("fields = %+v", fields[index])
		}
	}
}

func TestDiscordStockNotifierReportsDiscordFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	})}
	notifier := NewDiscordStockNotifier(client, "secret", StockDiscordChannels{Boss: "11"})
	notifier.baseURL = "https://discord.test"
	err := notifier.Notify(context.Background(), "123", []stock.Movement{{Safebox: "boss", ItemKey: "vector", Action: stock.ActionDeposit, Quantity: 1}}, nil)
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("error = %v", err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
