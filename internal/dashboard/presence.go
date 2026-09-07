package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MemberPresence is one member's live Discord state as the bot reports it.
type MemberPresence struct {
	DiscordUserID string     `json:"discord_user_id"`
	Status        string     `json:"status"`
	Playing       bool       `json:"playing"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
}

// PresenceClient reads live Discord presence from the bot.
//
// The API process has no gateway of its own, and presence is only ever wanted
// as of now, so it is pulled per request rather than pushed and cached: no
// staleness rule to get wrong, and nothing to reconcile after a restart. The
// bot serves it on a port that is not published outside the compose network,
// so there is no secret to hold either.
//
// This is what replaced storing every transition in activity_logs. That table
// keeps its history and is still the fallback for playtime before the game
// server started reporting, but nothing writes to it any more.
type PresenceClient struct {
	client *http.Client
	url    string
}

func NewPresenceClient(client *http.Client, url string) *PresenceClient {
	return &PresenceClient{client: client, url: url}
}

// Presences returns the live snapshot keyed by Discord user id.
//
// An unconfigured URL is not an error: a deployment without the bot reachable
// simply has no live presence, and the dashboard says so rather than failing.
func (c *PresenceClient) Presences(ctx context.Context) (map[string]MemberPresence, error) {
	if c == nil || c.url == "" {
		return nil, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build presence request: %w", err)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("read bot presence: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bot presence returned %s", response.Status)
	}

	var payload struct {
		Members []MemberPresence `json:"members"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode bot presence: %w", err)
	}
	presences := make(map[string]MemberPresence, len(payload.Members))
	for _, entry := range payload.Members {
		if entry.DiscordUserID != "" {
			presences[entry.DiscordUserID] = entry
		}
	}
	return presences, nil
}
