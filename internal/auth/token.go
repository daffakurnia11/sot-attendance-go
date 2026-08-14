package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	MemberID      int64  `json:"member_id"`
	DiscordUserID string `json:"discord_user_id"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
}

type Issuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewIssuer(secret string, ttl time.Duration) (*Issuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("token secret must contain at least 32 characters")
	}
	if ttl <= 0 {
		return nil, errors.New("token TTL must be positive")
	}
	return &Issuer{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

func (i *Issuer) Issue(found member.Member) (string, time.Time, error) {
	now := i.now().UTC()
	expiresAt := now.Add(i.ttl)
	claims := Claims{
		Issuer:        "sot-attendance-go",
		Audience:      "sot-attendance-fe",
		Subject:       strconv.FormatInt(found.ID, 10),
		MemberID:      found.ID,
		DiscordUserID: found.UserID,
		IssuedAt:      now.Unix(),
		ExpiresAt:     expiresAt.Unix(),
	}

	header, err := encodeJSON(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode token header: %w", err)
	}
	payload, err := encodeJSON(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode token claims: %w", err)
	}
	unsigned := header + "." + payload
	return unsigned + "." + i.sign(unsigned), expiresAt, nil
}

func (i *Issuer) Verify(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}
	expected := i.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return Claims{}, ErrInvalidToken
	}

	var header map[string]string
	if err := decodeJSON(parts[0], &header); err != nil || header["alg"] != "HS256" || header["typ"] != "JWT" {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := decodeJSON(parts[1], &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Issuer != "sot-attendance-go" || claims.Audience != "sot-attendance-fe" || claims.MemberID <= 0 || claims.Subject != strconv.FormatInt(claims.MemberID, 10) || claims.DiscordUserID == "" || claims.ExpiresAt <= i.now().Unix() {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (i *Issuer) sign(value string) string {
	mac := hmac.New(sha256.New, i.secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeJSON(value string, destination any) error {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, destination)
}
