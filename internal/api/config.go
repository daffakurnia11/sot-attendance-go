package api

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address       string
	JWTSecret     string
	JWTTTL        time.Duration
	FiveMCFXID    string
	FiveMPlayerID string
	// FiveMWebhookSecret is the shared HMAC secret the CR Roleplay server signs
	// its player log webhook requests with.
	FiveMWebhookSecret string
}

func LoadConfig() (Config, error) {
	return ConfigFromValues(
		os.Getenv("WEB_API_ADDRESS"),
		os.Getenv("APP_JWT_SECRET"),
		os.Getenv("APP_JWT_TTL"),
		os.Getenv("FIVEM_SERVER_CFX_ID"),
		os.Getenv("FIVEM_PLAYER_ID"),
		os.Getenv("FIVEM_WEBHOOK_SECRET"),
	)
}

func ConfigFromValues(address, jwtSecret, jwtTTL, fiveMCFXID, fiveMPlayerID, fiveMWebhookSecret string) (Config, error) {
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
	// The Cfx.re server code, the short identifier from a server's join link.
	// Restricted to the characters those codes use so a stray path segment or
	// query string cannot be appended to the directory URL.
	fiveMCFXID = strings.TrimSpace(fiveMCFXID)
	if fiveMCFXID == "" || !isCFXServerID(fiveMCFXID) {
		return Config{}, errors.New("FIVEM_SERVER_CFX_ID must be a Cfx.re server code such as kr7k7d")
	}
	fiveMPlayerID = strings.TrimSpace(fiveMPlayerID)
	if fiveMPlayerID == "" {
		return Config{}, errors.New("FIVEM_PLAYER_ID is required")
	}
	fiveMWebhookSecret = strings.TrimSpace(fiveMWebhookSecret)
	if len(fiveMWebhookSecret) < 32 {
		return Config{}, errors.New("FIVEM_WEBHOOK_SECRET must contain at least 32 characters")
	}
	if fiveMWebhookSecret == "replace-with-at-least-32-random-characters" {
		return Config{}, errors.New("FIVEM_WEBHOOK_SECRET must not use the example value")
	}
	return Config{Address: address, JWTSecret: jwtSecret, JWTTTL: ttl, FiveMCFXID: fiveMCFXID, FiveMPlayerID: fiveMPlayerID, FiveMWebhookSecret: fiveMWebhookSecret}, nil
}

func isCFXServerID(value string) bool {
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		default:
			return false
		}
	}
	return true
}
