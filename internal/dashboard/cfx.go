package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ParseCFXEndpoint validates FIVEM_SERVER_CFX_URL: the full Cfx.re directory
// URL for one server, such as
// https://frontend.cfx-services.net/api/servers/single/kr7k7d. The directory is
// reached over the internet rather than by connecting to the game server
// directly, so the host running this only needs outbound HTTPS.
func ParseCFXEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("FIVEM_SERVER_CFX_URL is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("FIVEM_SERVER_CFX_URL must be an absolute http(s) URL such as https://frontend.cfx-services.net/api/servers/single/kr7k7d")
	}
	return raw, nil
}

// NewCFXClient reads the roster from the Cfx.re directory endpoint validated
// by ParseCFXEndpoint.
func NewCFXClient(client *http.Client, endpoint, playerID string) *CFXClient {
	return &CFXClient{
		client:   client,
		endpoint: endpoint,
		playerID: strings.ToLower(playerID),
	}
}

func (c *CFXClient) Players(ctx context.Context) ([]CFXPlayer, error) {
	filtered, _, _, err := c.Rosters(ctx)
	return filtered, err
}

// Rosters returns the configured-family players, the complete live roster, and
// the player count the server reports for itself, from one upstream request.
//
// The count is returned because it is the only way to tell a healthy read from
// a truncated one. CFX answers 200 with whatever list it has cached, and a
// short list looks exactly like players having left. When the count and the
// list disagree, the list is incomplete and absence from it means nothing.
func (c *CFXClient) Rosters(ctx context.Context) ([]CFXPlayer, []CFXPlayer, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("create CFX players request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("fetch CFX players: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, 0, fmt.Errorf("fetch CFX players: unexpected HTTP status %d", response.StatusCode)
	}
	// A full server can list a thousand players, and this response is not one we
	// control, so the read is bounded.
	var payload struct {
		Data struct {
			Players []CFXPlayer `json:"players"`
			Clients int         `json:"clients"`
		} `json:"Data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, nil, 0, fmt.Errorf("decode CFX players: %w", err)
	}
	filtered := make([]CFXPlayer, 0)
	for _, player := range payload.Data.Players {
		if strings.Contains(strings.ToLower(player.Name), c.playerID) {
			filtered = append(filtered, player)
		}
	}
	return filtered, payload.Data.Players, payload.Data.Clients, nil
}
