package serverlog

import (
	"strings"
	"testing"
	"time"
)

func text(value string) *string { return &value }

func validEvent() Event {
	ping := 29
	return Event{
		Player: Player{
			ServerID: 142,
			Name:     "SOT - Ayvix",
			Username: "Kenji Nakamura",
			CID:      "CID-1024",
			Identifiers: Identifiers{
				License:  "license:kenji-smoketest",
				Discord:  "406954574998536202",
				FiveM:    "fivem:123123",
				SteamHex: "steam:123123123",
			},
			Ping: &ping,
		},
		Event: EventDetail{Type: "connecting", Timestamp: "2026-09-03T09:00:00Z"},
	}
}

var testPayload = []byte(`{"raw":"body"}`)

func TestValidateAcceptsEachStatus(t *testing.T) {
	for eventType, want := range map[string]string{
		"connecting": StatusConnecting, "connected": StatusConnected, "disconnected": StatusDisconnected,
		"player.connecting": StatusConnecting, "player.disconnected": StatusDisconnected,
	} {
		event := validEvent()
		event.Event.Type = eventType
		valid, err := Validate(testPayload, event)
		if err != nil {
			t.Fatalf("Validate(%s) error = %v", eventType, err)
		}
		if valid.Status != want {
			t.Fatalf("Validate(%s) status = %q, want %q", eventType, valid.Status, want)
		}
		if string(valid.Payload) != string(testPayload) {
			t.Fatalf("Validate() payload = %q", valid.Payload)
		}
	}
}

// All four identifiers are mandatory. Absent, null and blank all decode to the
// empty string and must be refused the same way.
func TestValidateRequiresEveryIdentifier(t *testing.T) {
	for _, blank := range []string{"", "   "} {
		for _, field := range []string{"license", "discord", "fivem", "steamhex"} {
			event := validEvent()
			switch field {
			case "license":
				event.Player.Identifiers.License = blank
			case "discord":
				event.Player.Identifiers.Discord = blank
			case "fivem":
				event.Player.Identifiers.FiveM = blank
			case "steamhex":
				event.Player.Identifiers.SteamHex = blank
			}
			if _, err := Validate(testPayload, event); err == nil {
				t.Fatalf("Validate() accepted %s = %q", field, blank)
			}
		}
	}
}

func TestValidateStripsDiscordPrefix(t *testing.T) {
	event := validEvent()
	event.Player.Identifiers.Discord = "discord:406954574998536202"
	valid, err := Validate(testPayload, event)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if valid.Discord != "406954574998536202" {
		t.Fatalf("Validate() discord = %q", valid.Discord)
	}
}

func TestValidateTimestampLayouts(t *testing.T) {
	want := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for _, value := range []string{"2026-09-03T09:00:00Z", "2026-09-03T09:00:00", "2026-09-03 09:00:00", "  2026-09-03T09:00:00Z  "} {
		event := validEvent()
		event.Event.Timestamp = value
		valid, err := Validate(testPayload, event)
		if err != nil {
			t.Fatalf("Validate(%q) error = %v", value, err)
		}
		if !valid.OccurredAt.Equal(want) {
			t.Fatalf("Validate(%q) occurred_at = %v", value, valid.OccurredAt)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Event)
	}{
		{"unknown type", func(e *Event) { e.Event.Type = "moved" }},
		{"blank type", func(e *Event) { e.Event.Type = "" }},
		{"bad timestamp", func(e *Event) { e.Event.Timestamp = "yesterday" }},
		{"blank timestamp", func(e *Event) { e.Event.Timestamp = "" }},
		{"empty player name", func(e *Event) { e.Player.Name = "" }},
		{"long player name", func(e *Event) { e.Player.Name = strings.Repeat("n", MaxPlayerName+1) }},
		{"long username", func(e *Event) { e.Player.Username = strings.Repeat("u", MaxUsername+1) }},
		{"long cid", func(e *Event) { e.Player.CID = strings.Repeat("c", MaxCID+1) }},
		{"license without prefix", func(e *Event) { e.Player.Identifiers.License = "abcdefghij" }},
		{"short discord", func(e *Event) { e.Player.Identifiers.Discord = "1234" }},
		{"non numeric discord", func(e *Event) { e.Player.Identifiers.Discord = "12345678901234567x" }},
		{"fivem without prefix", func(e *Event) { e.Player.Identifiers.FiveM = "abcdefg" }},
		{"steam without prefix", func(e *Event) { e.Player.Identifiers.SteamHex = "abcdefg" }},
		{"control character in name", func(e *Event) { e.Player.Name = "SOT\x00Ayvix" }},
		{"invalid utf8 in cid", func(e *Event) { e.Player.CID = string([]byte{0xff, 0xfe}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			event := validEvent()
			test.mutate(&event)
			if _, err := Validate(testPayload, event); err == nil {
				t.Fatalf("Validate() error = nil, want rejection")
			}
		})
	}
}

// Error text reaches the sender, so it must name the field without echoing the
// value it rejected.
func TestValidationErrorDoesNotEchoValue(t *testing.T) {
	event := validEvent()
	event.Player.Identifiers.License = "secret-identifier-value"

	_, err := Validate(testPayload, event)
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	if strings.Contains(err.Error(), "secret-identifier-value") {
		t.Fatalf("validation error echoed the rejected value: %q", err.Error())
	}
}

// player.ping, player.server_id and event.reason stay accepted on the wire so
// the FiveM script need not strip them, but nothing stores them now.
func TestValidateIgnoresUnstoredFields(t *testing.T) {
	event := validEvent()
	event.Player.ServerID = -1
	huge := MaxPing * 99
	event.Player.Ping = &huge
	reason := strings.Repeat("r", MaxDisconnectReason*2)
	event.Event.Reason = &reason

	if _, err := Validate(testPayload, event); err != nil {
		t.Fatalf("Validate() rejected a field it no longer stores: %v", err)
	}
}
