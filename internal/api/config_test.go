package api

import (
	"testing"
	"time"
)

func TestConfigFromValues(t *testing.T) {
	secret := "01234567890123456789012345678901"
	webhookSecret := secret + "-webhook"
	config, err := ConfigFromValues("", secret, "", webhookSecret)
	if err != nil {
		t.Fatalf("ConfigFromValues() error = %v", err)
	}
	if config.Address != ":8080" || config.JWTTTL != 15*time.Minute || config.JWTSecret != secret {
		t.Fatalf("ConfigFromValues() = %#v", config)
	}
	if config.FiveMWebhookSecret != webhookSecret {
		t.Fatalf("ConfigFromValues() webhook secret = %q", config.FiveMWebhookSecret)
	}

	for _, bad := range []string{"", "   ", "short", "replace-with-at-least-32-random-characters"} {
		if _, err := ConfigFromValues(":8080", secret, "15m", bad); err == nil {
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
		if _, err := ConfigFromValues(":8080", test.secret, test.ttl, webhookSecret); err == nil {
			t.Fatalf("ConfigFromValues(%q, %q) error = nil", test.secret, test.ttl)
		}
	}
}
