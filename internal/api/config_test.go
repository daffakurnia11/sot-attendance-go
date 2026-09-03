package api

import (
	"testing"
	"time"
)

func TestConfigFromValues(t *testing.T) {
	secret := "01234567890123456789012345678901"
	webhookSecret := secret + "-webhook"
	config, err := ConfigFromValues("", secret, "", "kr7k7d", "SOT", webhookSecret)
	if err != nil {
		t.Fatalf("ConfigFromValues() error = %v", err)
	}
	if config.Address != ":8080" || config.JWTTTL != 15*time.Minute || config.JWTSecret != secret || config.FiveMCFXID != "kr7k7d" || config.FiveMPlayerID != "SOT" {
		t.Fatalf("ConfigFromValues() = %#v", config)
	}
	if config.FiveMWebhookSecret != webhookSecret {
		t.Fatalf("ConfigFromValues() webhook secret = %q", config.FiveMWebhookSecret)
	}

	for _, bad := range []string{"", "   ", "short", "replace-with-at-least-32-random-characters"} {
		if _, err := ConfigFromValues(":8080", secret, "15m", "kr7k7d", "SOT", bad); err == nil {
			t.Fatalf("ConfigFromValues() accepted FIVEM_WEBHOOK_SECRET %q", bad)
		}
	}

	for _, test := range []struct{ secret, ttl string }{
		{"short", "15m"},
		{"replace-with-at-least-32-random-characters", "15m"},
		{secret, "invalid"},
		{secret, "0s"},
		{secret, "25h"},
	} {
		if _, err := ConfigFromValues(":8080", test.secret, test.ttl, "kr7k7d", "SOT", webhookSecret); err == nil {
			t.Fatalf("ConfigFromValues(%q, %q) error = nil", test.secret, test.ttl)
		}
	}
	// A server code goes straight into the directory URL, so anything carrying
	// a path, query, or scheme has to be refused rather than escaped.
	for _, values := range [][2]string{
		{"", "SOT"},
		{"kr7k7d/../other", "SOT"},
		{"kr7k7d?q=1", "SOT"},
		{"http://127.0.0.1:30120", "SOT"},
		{"kr7k7d", ""},
	} {
		if _, err := ConfigFromValues(":8080", secret, "15m", values[0], values[1], webhookSecret); err == nil {
			t.Fatalf("ConfigFromValues() accepted FiveM values %#v", values)
		}
	}
}
