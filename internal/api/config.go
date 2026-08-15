package api

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address        string
	JWTSecret      string
	JWTTTL         time.Duration
	FiveMServerURL string
	FiveMPlayerID  string
}

func LoadConfig() (Config, error) {
	return ConfigFromValues(
		os.Getenv("WEB_API_ADDRESS"),
		os.Getenv("APP_JWT_SECRET"),
		os.Getenv("APP_JWT_TTL"),
		os.Getenv("FIVEM_SERVER_IP"),
		os.Getenv("FIVEM_PLAYER_ID"),
	)
}

func ConfigFromValues(address, jwtSecret, jwtTTL, fiveMServerURL, fiveMPlayerID string) (Config, error) {
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
	fiveMServerURL = strings.TrimRight(strings.TrimSpace(fiveMServerURL), "/")
	parsedURL, err := url.Parse(fiveMServerURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return Config{}, errors.New("FIVEM_SERVER_IP must be a valid HTTP or HTTPS URL")
	}
	fiveMPlayerID = strings.TrimSpace(fiveMPlayerID)
	if fiveMPlayerID == "" {
		return Config{}, errors.New("FIVEM_PLAYER_ID is required")
	}
	return Config{Address: address, JWTSecret: jwtSecret, JWTTTL: ttl, FiveMServerURL: fiveMServerURL, FiveMPlayerID: fiveMPlayerID}, nil
}
