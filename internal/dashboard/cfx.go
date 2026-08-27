package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// cfxServerEndpoint is the public Cfx.re server directory. It is reached over
// the internet rather than by connecting to the game server directly, so the
// host running this only needs outbound HTTPS.
const cfxServerEndpoint = "https://frontend.cfx-services.net/api/servers/single/"

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

// NewCFXClient reads the roster from the Cfx.re directory by server code, the
// short identifier in a server's join link.
func NewCFXClient(client *http.Client, cfxServerID, playerID string) *CFXClient {
	return &CFXClient{
		client:   client,
		endpoint: cfxServerEndpoint + url.PathEscape(cfxServerID),
		playerID: strings.ToLower(playerID),
	}
}

func (c *CFXClient) Players(ctx context.Context) ([]CFXPlayer, error) {
	filtered, _, err := c.Rosters(ctx)
	return filtered, err
}

// Rosters returns both configured-family players and complete live server roster
// from one upstream request.
func (c *CFXClient) Rosters(ctx context.Context) ([]CFXPlayer, []CFXPlayer, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create CFX players request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch CFX players: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch CFX players: unexpected HTTP status %d", response.StatusCode)
	}
	// A full server can list a thousand players, and this response is not one we
	// control, so the read is bounded.
	var payload struct {
		Data struct {
			Players []CFXPlayer `json:"players"`
		} `json:"Data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("decode CFX players: %w", err)
	}
	filtered := make([]CFXPlayer, 0)
	for _, player := range payload.Data.Players {
		if strings.Contains(strings.ToLower(player.Name), c.playerID) {
			filtered = append(filtered, player)
		}
	}
	return filtered, payload.Data.Players, nil
}
