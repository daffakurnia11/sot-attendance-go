package api

import (
	"context"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"net/http"
	"testing"
)

type stubPriceWriter struct {
	stubBusinessCatalog
	err    error
	actor  int64
	called bool
}

func (s *stubPriceWriter) Update(_ context.Context, u business.Update, actor int64) (int64, error) {
	s.called = true
	s.actor = actor
	return u.Version + 1, s.err
}
func TestPriceManagementAuthorizationAndConflict(t *testing.T) {
	for _, tc := range []struct {
		name   string
		admin  bool
		err    error
		status int
	}{{"member denied", false, nil, 403}, {"admin saved", true, nil, 200}, {"stale edit", true, business.ErrConflict, 409}, {"duplicate", true, business.ErrDuplicate, 409}, {"missing", true, business.ErrNotFound, 404}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubPriceWriter{err: tc.err}
			h := NewHandler(Deps{Tokens: stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, Members: &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: tc.admin}}, Business: store, Logger: testLogger()})
			r := requestWithBody(h, http.MethodPatch, "/api/v1/price-notes", "Bearer token", `{"id":1,"direction":"sale","version":1,"price":5000,"unit":"piece","notes":"20/week"}`)
			if r.Code != tc.status {
				t.Fatalf("response=%d %s", r.Code, r.Body.String())
			}
			if !tc.admin && store.called {
				t.Fatal("member reached write")
			}
			if tc.admin && store.actor != 7 {
				t.Fatal("actor not recorded")
			}
		})
	}
}

func TestInternalPriceManagement(t *testing.T) {
	for _, admin := range []bool{false, true} {
		store := &stubPriceWriter{}
		h := NewHandler(Deps{Tokens: stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, Members: &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: admin}}, Business: store, Logger: testLogger()})
		response := requestWithBody(h, http.MethodPatch, "/api/v1/price-notes", "Bearer token", `{"id":1,"direction":"internal","version":1,"price":18500,"unit":"package","notes":"Updated member package"}`)
		if admin && (response.Code != 200 || !store.called || store.actor != 7) {
			t.Fatalf("admin update rejected: %d", response.Code)
		}
		if !admin && (response.Code != 403 || store.called) {
			t.Fatal("non-admin internal write allowed")
		}
	}
}
