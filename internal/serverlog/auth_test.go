package serverlog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func fixedAuthenticator(secret string, now time.Time) *Authenticator {
	return NewAuthenticator(secret, func() time.Time { return now })
}

func TestAuthenticateAcceptsSecret(t *testing.T) {
	auth := fixedAuthenticator(testSecret, time.Unix(1_770_000_000, 0).UTC())

	for _, provided := range []string{testSecret, "  " + testSecret + "  ", "Bearer " + testSecret} {
		if err := auth.Authenticate(provided); err != nil {
			t.Fatalf("Authenticate(%q) error = %v", provided, err)
		}
	}
}

func TestAuthenticateRejects(t *testing.T) {
	auth := fixedAuthenticator(testSecret, time.Unix(1_770_000_000, 0).UTC())

	for _, test := range []struct{ name, provided string }{
		{"empty", ""},
		{"blank", "   "},
		{"wrong secret", strings.Repeat("f", 32)},
		{"prefix of the secret", testSecret[:16]},
		{"secret plus suffix", testSecret + "x"},
		{"case changed", strings.ToUpper(testSecret)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := auth.Authenticate(test.provided); !errors.Is(err, ErrInvalidSecret) {
				t.Fatalf("Authenticate(%q) error = %v, want ErrInvalidSecret", test.provided, err)
			}
		})
	}
}

// NewAuthenticator copies the secret so a later mutation cannot widen what is
// accepted.
func TestAuthenticatorCopiesSecret(t *testing.T) {
	secret := testSecret
	auth := fixedAuthenticator(secret, time.Unix(1_770_000_000, 0).UTC())
	secret = "something-else-entirely-abcdefgh"

	if err := auth.Authenticate(testSecret); err != nil {
		t.Fatalf("Authenticate() after mutating the source string error = %v", err)
	}
	_ = secret
}

func TestFresh(t *testing.T) {
	now := time.Unix(1_770_000_000, 0).UTC()
	auth := fixedAuthenticator(testSecret, now)

	for _, test := range []struct {
		name   string
		moment time.Time
		want   bool
	}{
		{"now", now, true},
		{"just inside past", now.Add(-TimestampSkew), true},
		{"just inside future", now.Add(TimestampSkew), true},
		{"stale", now.Add(-TimestampSkew - time.Second), false},
		{"too far ahead", now.Add(TimestampSkew + time.Second), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := auth.Fresh(test.moment); got != test.want {
				t.Fatalf("Fresh(%v) = %v, want %v", test.moment, got, test.want)
			}
		})
	}
}
