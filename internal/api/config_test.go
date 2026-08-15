package api

import (
	"testing"
	"time"
)

func TestConfigFromValues(t *testing.T) {
	secret := "01234567890123456789012345678901"
	config, err := ConfigFromValues("", secret, "", "http://127.0.0.1:30120", "SOT")
	if err != nil {
		t.Fatalf("ConfigFromValues() error = %v", err)
	}
	if config.Address != ":8080" || config.JWTTTL != 15*time.Minute || config.JWTSecret != secret || config.FiveMPlayerID != "SOT" {
		t.Fatalf("ConfigFromValues() = %#v", config)
	}

	for _, test := range []struct{ secret, ttl string }{
		{"short", "15m"},
		{"replace-with-at-least-32-random-characters", "15m"},
		{secret, "invalid"},
		{secret, "0s"},
		{secret, "25h"},
	} {
		if _, err := ConfigFromValues(":8080", test.secret, test.ttl, "http://127.0.0.1:30120", "SOT"); err == nil {
			t.Fatalf("ConfigFromValues(%q, %q) error = nil", test.secret, test.ttl)
		}
	}
	for _, values := range [][2]string{{"", "SOT"}, {"127.0.0.1:30120", "SOT"}, {"ftp://server", "SOT"}, {"http://server", ""}} {
		if _, err := ConfigFromValues(":8080", secret, "15m", values[0], values[1]); err == nil {
			t.Fatalf("ConfigFromValues() accepted FiveM values %#v", values)
		}
	}
}
