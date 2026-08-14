package api

import (
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address   string
	JWTSecret string
	JWTTTL    time.Duration
}

func LoadConfig() (Config, error) {
	return ConfigFromValues(
		os.Getenv("WEB_API_ADDRESS"),
		os.Getenv("APP_JWT_SECRET"),
		os.Getenv("APP_JWT_TTL"),
	)
}

func ConfigFromValues(address, jwtSecret, jwtTTL string) (Config, error) {
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
	return Config{Address: address, JWTSecret: jwtSecret, JWTTTL: ttl}, nil
}
