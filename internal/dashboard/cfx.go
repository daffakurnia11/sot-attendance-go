package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type CFXPlayer struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Ping int    `json:"ping"`
}

type CFXClient struct {
	client   *http.Client
	endpoint string
	playerID string
}

func NewCFXClient(client *http.Client, serverURL, playerID string) *CFXClient {
	endpoint, _ := url.JoinPath(serverURL, "players.json")
	return &CFXClient{client: client, endpoint: endpoint, playerID: strings.ToLower(playerID)}
}

func (c *CFXClient) Players(ctx context.Context) ([]CFXPlayer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create CFX players request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch CFX players: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch CFX players: unexpected HTTP status %d", response.StatusCode)
	}
	var allPlayers []CFXPlayer
	if err := json.NewDecoder(response.Body).Decode(&allPlayers); err != nil {
		return nil, fmt.Errorf("decode CFX players: %w", err)
	}
	filtered := make([]CFXPlayer, 0)
	for _, player := range allPlayers {
		if strings.Contains(strings.ToLower(player.Name), c.playerID) {
			filtered = append(filtered, player)
		}
	}
	return filtered, nil
}
