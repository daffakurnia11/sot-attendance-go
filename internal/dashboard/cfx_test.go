package dashboard

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestCFXClientFiltersPlayerNamesCaseInsensitively(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/players.json" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[
			{"id":12,"name":"SOT - Paw","ping":48},
			{"id":181,"name":"sotxYWG - s4chie","ping":16},
			{"id":7,"name":"Unrelated","ping":20}
		]`)), Header: make(http.Header)}, nil
	})}

	players, err := NewCFXClient(client, "http://server.test", "SOT").Players(context.Background())
	if err != nil {
		t.Fatalf("Players() error = %v", err)
	}
	if len(players) != 2 || players[0].ID != 12 || players[1].ID != 181 {
		t.Fatalf("Players() = %#v", players)
	}
}

func TestCFXClientRejectsUpstreamFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}

	if _, err := NewCFXClient(client, "http://server.test", "SOT").Players(context.Background()); err == nil {
		t.Fatal("Players() error = nil")
	}
}
