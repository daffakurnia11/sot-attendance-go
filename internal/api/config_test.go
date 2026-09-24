package api

import (
	"testing"
	"time"
)

func TestConfigFromValues(t *testing.T) {
	secret := "01234567890123456789012345678901"
	webhookSecret := secret + "-webhook"
	config, err := ConfigFromValues("", secret, "", "https://frontend.cfx-services.net/api/servers/single/kr7k7d", "SOT", webhookSecret)
	if err != nil {
		t.Fatalf("ConfigFromValues() error = %v", err)
	}
	if config.Address != ":8080" || config.JWTTTL != 15*time.Minute || config.JWTSecret != secret || config.FiveMCFXEndpoint != "https://frontend.cfx-services.net/api/servers/single/kr7k7d" || config.FiveMPlayerID != "SOT" {
		t.Fatalf("ConfigFromValues() = %#v", config)
	}
	if config.FiveMWebhookSecret != webhookSecret {
		t.Fatalf("ConfigFromValues() webhook secret = %q", config.FiveMWebhookSecret)
	}

	for _, bad := range []string{"", "   ", "short", "replace-with-at-least-32-random-characters"} {
		if _, err := ConfigFromValues(":8080", secret, "15m", "https://frontend.cfx-services.net/api/servers/single/kr7k7d", "SOT", bad); err == nil {
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
		if _, err := ConfigFromValues(":8080", test.secret, test.ttl, "https://frontend.cfx-services.net/api/servers/single/kr7k7d", "SOT", webhookSecret); err == nil {
			t.Fatalf("ConfigFromValues(%q, %q) error = nil", test.secret, test.ttl)
		}
	}
	// The endpoint is requested as written, so anything that is not an
	// absolute http(s) URL is refused at startup rather than at the first poll.
	for _, values := range [][2]string{
		{"", "SOT"},
		{"kr7k7d", "SOT"},
		{"/api/servers/single/kr7k7d", "SOT"},
		{"ftp://frontend.cfx-services.net/x", "SOT"},
		{"https://frontend.cfx-services.net/api/servers/single/kr7k7d", ""},
	} {
		if _, err := ConfigFromValues(":8080", secret, "15m", values[0], values[1], webhookSecret); err == nil {
			t.Fatalf("ConfigFromValues() accepted FiveM values %#v", values)
		}
	}
}
