package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDiscordVerifier(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantID  string
		wantErr error
	}{
		{name: "valid", status: http.StatusOK, body: `{"id":"123456","username":"delta"}`, wantID: "123456"},
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{}`, wantErr: ErrInvalidDiscordToken},
		{name: "upstream failure", status: http.StatusServiceUnavailable, body: `{}`, wantErr: ErrDiscordUnavailable},
		{name: "invalid identity", status: http.StatusOK, body: `{"id":"invalid","username":"delta"}`, wantErr: ErrDiscordUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
				}
				return &http.Response{
					StatusCode: test.status,
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Header:     make(http.Header),
				}, nil
			})}

			verifier := NewDiscordVerifier(client)
			user, err := verifier.Verify(context.Background(), "secret")
			if !errors.Is(err, test.wantErr) || user.ID != test.wantID {
				t.Fatalf("Verify() = %#v, %v", user, err)
			}
		})
	}
}
