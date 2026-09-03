package serverlog

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidSecret means X-SOT-Secret was missing or did not match.
	ErrInvalidSecret = errors.New("webhook secret is missing or does not match")
	// ErrExpiredTimestamp means event.timestamp sat outside the allowed window.
	ErrExpiredTimestamp = errors.New("event timestamp outside allowed window")
)

// Authenticator checks the shared secret a sender presents in a header.
//
// This is bearer-token authentication, not a signature: the secret travels on
// every request. It is deliberately simple so the FiveM Lua resource needs no
// HMAC implementation, and it relies on TLS for confidentiality. Two things it
// therefore cannot do, which an HMAC could:
//
//   - prove the body was not altered in transit
//   - keep the secret out of any proxy or access log that records headers
//
// Serve this endpoint over HTTPS only.
type Authenticator struct {
	secret []byte
	now    func() time.Time
}

// NewAuthenticator copies the secret so a later mutation of the caller's string
// cannot change what is accepted. now may be nil, in which case time.Now is
// used.
func NewAuthenticator(secret string, now func() time.Time) *Authenticator {
	if now == nil {
		now = time.Now
	}
	return &Authenticator{secret: []byte(secret), now: now}
}

// Authenticate reports whether provided matches the configured secret.
//
// The comparison is constant time: a byte-by-byte compare would leak the secret
// one character at a time to anyone able to measure response latency.
func (a *Authenticator) Authenticate(provided string) error {
	trimmed := []byte(strings.TrimSpace(strings.TrimPrefix(provided, "Bearer ")))
	if len(trimmed) == 0 {
		return ErrInvalidSecret
	}
	if subtle.ConstantTimeCompare(trimmed, a.secret) != 1 {
		return ErrInvalidSecret
	}
	return nil
}

// Fresh reports whether moment sits inside the allowed window of server time.
//
// With no signature covering the body, this is the only bound on replaying a
// captured request. It is not a strong guarantee: anyone holding the secret can
// mint a fresh timestamp. It limits accidental redelivery and stale queues.
func (a *Authenticator) Fresh(moment time.Time) bool {
	drift := a.now().UTC().Sub(moment.UTC())
	if drift < 0 {
		drift = -drift
	}
	return drift <= TimestampSkew
}
