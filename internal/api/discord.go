package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	ErrInvalidDiscordToken = errors.New("invalid Discord access token")
	ErrDiscordUnavailable  = errors.New("Discord API unavailable")
)

type DiscordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type DiscordVerifier struct {
	client   *http.Client
	endpoint string
}

func NewDiscordVerifier(client *http.Client) *DiscordVerifier {
	return &DiscordVerifier{client: client, endpoint: "https://discord.com/api/v10/users/@me"}
}

func (v *DiscordVerifier) Verify(ctx context.Context, accessToken string) (DiscordUser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.endpoint, nil)
	if err != nil {
		return DiscordUser{}, fmt.Errorf("create Discord request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")

	response, err := v.client.Do(request)
	if err != nil {
		return DiscordUser{}, fmt.Errorf("%w: %v", ErrDiscordUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return DiscordUser{}, ErrInvalidDiscordToken
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return DiscordUser{}, fmt.Errorf("%w: status %d", ErrDiscordUnavailable, response.StatusCode)
	}

	var user DiscordUser
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&user); err != nil {
		return DiscordUser{}, fmt.Errorf("%w: decode response", ErrDiscordUnavailable)
	}
	if !isDiscordID(user.ID) || strings.TrimSpace(user.Username) == "" {
		return DiscordUser{}, fmt.Errorf("%w: invalid user response", ErrDiscordUnavailable)
	}
	return user, nil
}

func isDiscordID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
