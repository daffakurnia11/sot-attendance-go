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
		if request.URL.String() != cfxServerEndpoint+"kr7k7d" {
			t.Fatalf("url = %q", request.URL.String())
		}
		// The directory nests the roster under Data, unlike the game server's
		// own players.json, which returns a bare array.
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Data":{"clients":3,"players":[
			{"id":12,"name":"SOT - Paw","ping":48},
			{"id":181,"name":"sotxYWG - s4chie","ping":16},
			{"id":7,"name":"Unrelated","ping":20}
		]}}`)), Header: make(http.Header)}, nil
	})}

	players, err := NewCFXClient(client, "kr7k7d", "SOT").Players(context.Background())
	if err != nil {
		t.Fatalf("Players() error = %v", err)
	}
	if len(players) != 2 || players[0].ID != 12 || players[1].ID != 181 {
		t.Fatalf("Players() = %#v", players)
	}
}

func TestCFXClientReturnsEmptyRosterWhenServerIsOffline(t *testing.T) {
	// An offline server still resolves in the directory but reports no roster.
	// That is an empty list, not a failure, so the dashboard keeps showing CFX
	// as available rather than falling back to unavailable.
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"Data":{"players":[]}}`)), Header: make(http.Header)}, nil
	})}

	players, err := NewCFXClient(client, "kr7k7d", "SOT").Players(context.Background())
	if err != nil {
		t.Fatalf("Players() error = %v", err)
	}
	if len(players) != 0 {
		t.Fatalf("Players() = %#v, want empty", players)
	}
}

func TestCFXClientRejectsUpstreamFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}

	if _, err := NewCFXClient(client, "kr7k7d", "SOT").Players(context.Background()); err == nil {
		t.Fatal("Players() error = nil")
	}
}
