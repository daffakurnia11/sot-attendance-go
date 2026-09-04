package serverlog

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidationError is a field-level rejection. Its message names the field and
// the rule it broke and never echoes the offending value, so it is safe to
// return to the sender and safe to log.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Validate checks the payload and normalises it.
//
// It is deliberately lenient about shape where leniency costs nothing: any
// optional field may be absent, empty, or JSON null, because Lua JSON encoders
// spell a missing value all three ways. It is strict only where a wrong value
// would corrupt stored data.
//
// payload is the exact request body, stored as-is.
func Validate(payload []byte, event Event) (ValidEvent, error) {
	status, known := eventTypeToStatus[strings.TrimSpace(event.Event.Type)]
	if !known {
		return ValidEvent{}, invalid("event.type must be connecting, connected, or disconnected")
	}

	occurredAt, err := parseTimestamp(event.Event.Timestamp)
	if err != nil {
		return ValidEvent{}, err
	}

	if err := checkText("player.name", event.Player.Name, 1, MaxPlayerName); err != nil {
		return ValidEvent{}, err
	}
	if err := checkText("player.username", event.Player.Username, 1, MaxUsername); err != nil {
		return ValidEvent{}, err
	}
	if err := checkText("player.cid", event.Player.CID, 1, MaxCID); err != nil {
		return ValidEvent{}, err
	}

	license := strings.TrimSpace(event.Player.Identifiers.License)
	if err := checkText("player.identifiers.license", license, MinLicense, MaxLicense); err != nil {
		return ValidEvent{}, err
	}
	if !strings.HasPrefix(license, "license:") {
		return ValidEvent{}, invalid("player.identifiers.license must start with license:")
	}

	discord, err := requiredDiscord(event.Player.Identifiers.Discord)
	if err != nil {
		return ValidEvent{}, err
	}
	// fivem is stored but not validated: it is absent whenever the player has
	// no CFX account attached, and refusing the whole event over a decorative
	// identifier lost the player from attendance entirely. Only text safety is
	// still enforced, so nothing hostile reaches the database.
	fivem, err := optionalRaw("player.identifiers.fivem", event.Player.Identifiers.FiveM, MaxFiveMID)
	if err != nil {
		return ValidEvent{}, err
	}
	steamhex, err := requiredPrefixed("player.identifiers.steamhex", event.Player.Identifiers.SteamHex, "steam:", MaxSteamHex)
	if err != nil {
		return ValidEvent{}, err
	}

	// player.ping, player.server_id and event.reason are accepted on the wire so
	// the sender need not strip them, but nothing stores them, so nothing
	// validates them either.

	return ValidEvent{
		Payload:    payload,
		Status:     status,
		OccurredAt: occurredAt,
		PlayerName: event.Player.Name,
		Username:   event.Player.Username,
		CID:        event.Player.CID,
		License:    license,
		Discord:    discord,
		FiveM:      fivem,
		SteamHex:   steamhex,
	}, nil
}

// parseTimestamp accepts RFC3339 and the same value without a zone, which is
// what os.date("!%Y-%m-%dT%H:%M:%SZ") and its near misses produce in Lua.
func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, invalid("event.timestamp must be an RFC3339 UTC timestamp")
}

// requiredDiscord tolerates the "discord:" prefix that GetPlayerIdentifiers
// returns, but the value itself is mandatory.
func requiredDiscord(value string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "discord:")
	if trimmed == "" {
		return "", invalid("player.identifiers.discord is required")
	}
	if length := len(trimmed); length < 17 || length > 20 {
		return "", invalid("player.identifiers.discord must be 17 to 20 digits")
	}
	for _, character := range trimmed {
		if character < '0' || character > '9' {
			return "", invalid("player.identifiers.discord must be 17 to 20 digits")
		}
	}
	return trimmed, nil
}

// optionalRaw accepts any text, normalising absent, null and blank to nil. It
// still rejects invalid UTF-8, control characters and over-long values, which
// is hygiene rather than format validation.
func optionalRaw(field, value string, max int) (*string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if err := checkText(field, trimmed, 1, max); err != nil {
		return nil, err
	}
	return &trimmed, nil
}

func requiredPrefixed(field, value, prefix string, max int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", invalid("%s is required", field)
	}
	if err := checkText(field, trimmed, MinPrefixedID, max); err != nil {
		return "", err
	}
	if !strings.HasPrefix(trimmed, prefix) {
		return "", invalid("%s must start with %s", field, prefix)
	}
	return trimmed, nil
}

// checkText rejects invalid UTF-8, control characters, and out-of-range
// lengths. Length is counted in runes, matching the contract's "characters".
func checkText(field, value string, min, max int) error {
	if !utf8.ValidString(value) {
		return invalid("%s must be valid UTF-8", field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return invalid("%s must not contain control characters", field)
		}
	}
	if length := utf8.RuneCountInString(value); length < min || length > max {
		return invalid("%s must be %d to %d characters", field, min, max)
	}
	return nil
}
