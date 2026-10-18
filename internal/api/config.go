package api

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address   string
	JWTSecret string
	JWTTTL    time.Duration
	// FiveMWebhookSecret is the shared HMAC secret the CR Roleplay server signs
	// its player log webhook requests with.
	FiveMWebhookSecret string
	// BlacklistedSteamHex holds lowercased steam:... identifiers whose webhook
	// events are acknowledged and dropped. FIVEM_BLACKLISTED_STEAMHEX, comma
	// separated.
	BlacklistedSteamHex map[string]bool
}

func LoadConfig() (Config, error) {
	config, err := ConfigFromValues(
		os.Getenv("WEB_API_ADDRESS"),
		os.Getenv("APP_JWT_SECRET"),
		os.Getenv("APP_JWT_TTL"),
		os.Getenv("FIVEM_WEBHOOK_SECRET"),
	)
	if err != nil {
		return Config{}, err
	}
	config.BlacklistedSteamHex, err = parseSteamHexList(os.Getenv("FIVEM_BLACKLISTED_STEAMHEX"))
	return config, err
}

// parseSteamHexList fails on an entry without the steam: prefix: a typo would
// otherwise silently let the player through.
func parseSteamHexList(value string) (map[string]bool, error) {
	list := make(map[string]bool)
	for _, entry := range strings.Split(value, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if !strings.HasPrefix(entry, "steam:") || len(entry) == len("steam:") {
			return nil, fmt.Errorf("FIVEM_BLACKLISTED_STEAMHEX entry %q must look like steam:1100001...", entry)
		}
		list[entry] = true
	}
	return list, nil
}

func ConfigFromValues(address, jwtSecret, jwtTTL, fiveMWebhookSecret string) (Config, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = ":8080"
	}
	jwtSecret = strings.TrimSpace(jwtSecret)
	if len(jwtSecret) < 32 {
		return Config{}, errors.New("APP_JWT_SECRET must contain at least 32 characters")
	}
	if jwtSecret == "replace-with-at-least-32-random-characters" {
		return Config{}, errors.New("APP_JWT_SECRET must not use the example value")
	}
	jwtTTL = strings.TrimSpace(jwtTTL)
	if jwtTTL == "" {
		jwtTTL = "15m"
	}
	ttl, err := time.ParseDuration(jwtTTL)
	if err != nil || ttl <= 0 || ttl > 24*time.Hour {
		return Config{}, errors.New("APP_JWT_TTL must be a positive Go duration no longer than 24h")
	}
	fiveMWebhookSecret = strings.TrimSpace(fiveMWebhookSecret)
	if len(fiveMWebhookSecret) < 32 {
		return Config{}, errors.New("FIVEM_WEBHOOK_SECRET must contain at least 32 characters")
	}
	if fiveMWebhookSecret == "replace-with-at-least-32-random-characters" {
		return Config{}, errors.New("FIVEM_WEBHOOK_SECRET must not use the example value")
	}
	return Config{Address: address, JWTSecret: jwtSecret, JWTTTL: ttl, FiveMWebhookSecret: fiveMWebhookSecret}, nil
}
