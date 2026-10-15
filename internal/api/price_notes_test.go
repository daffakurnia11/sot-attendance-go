package api

import (
	"context"
	"errors"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"net/http"
	"strings"
	"testing"
)

type stubBusinessCatalog struct{ err error }

func (s stubBusinessCatalog) List(context.Context) ([]business.Entry, error) {
	return []business.Entry{{ID: 1, PartnerCode: "BZ", PartnerName: "BLADEZAY", Direction: "purchase", Name: "Ammo Rifle", Price: 3100, Unit: "clip", Notes: "180 rounds/clip"}, {ID: 2, PartnerCode: "BZ", Direction: "sale", Name: "Vector", Price: 8000, Unit: "piece", Notes: "20/week (shared)"}}, s.err
}
func TestPriceNotesMemberAccessAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		store       businessCatalogReader
		status      int
	}{
		{"member", "Bearer token", stubBusinessCatalog{}, http.StatusOK},
		{"anonymous", "", stubBusinessCatalog{}, http.StatusUnauthorized},
		{"unavailable", "Bearer token", nil, http.StatusServiceUnavailable},
		{"repository error", "Bearer token", stubBusinessCatalog{errors.New("database error")}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(Deps{Tokens: stubTokens{claims: appauth.Claims{MemberID: 7}}, Business: tc.store, Logger: testLogger()})
			response := request(h, http.MethodGet, "/api/v1/price-notes", tc.token)
			if response.Code != tc.status {
				t.Fatalf("response=%d %s", response.Code, response.Body.String())
			}
			if tc.status == http.StatusOK && (!strings.Contains(response.Body.String(), `"direction":"purchase"`) || !strings.Contains(response.Body.String(), `"direction":"sale"`)) {
				t.Fatalf("missing catalog direction: %s", response.Body.String())
			}
		})
	}
}
