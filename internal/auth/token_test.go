package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

func TestIssueAndVerify(t *testing.T) {
	issuer, err := NewIssuer("01234567890123456789012345678901", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	issuer.now = func() time.Time { return now }

	token, expiresAt, err := issuer.Issue(member.Member{ID: 42, UserID: "123456"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.MemberID != 42 || claims.DiscordUserID != "123456" || !expiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("claims = %#v, expiresAt = %v", claims, expiresAt)
	}
}

func TestVerifyRejectsTamperedAndExpiredTokens(t *testing.T) {
	issuer, _ := NewIssuer("01234567890123456789012345678901", time.Minute)
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	issuer.now = func() time.Time { return now }
	token, _, _ := issuer.Issue(member.Member{ID: 1, UserID: "123"})

	if _, err := issuer.Verify(token + "tampered"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(tampered) error = %v", err)
	}
	issuer.now = func() time.Time { return now.Add(time.Minute) }
	if _, err := issuer.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Verify(expired) error = %v", err)
	}
}

func TestNewIssuerValidatesConfiguration(t *testing.T) {
	if _, err := NewIssuer("short", time.Minute); err == nil {
		t.Fatal("NewIssuer(short secret) error = nil")
	}
	if _, err := NewIssuer("01234567890123456789012345678901", 0); err == nil {
		t.Fatal("NewIssuer(zero TTL) error = nil")
	}
}
