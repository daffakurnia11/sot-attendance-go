package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

type stubVerifier struct {
	user  DiscordUser
	error error
	token string
}

func (v *stubVerifier) Verify(_ context.Context, token string) (DiscordUser, error) {
	v.token = token
	return v.user, v.error
}

type stubMembers struct {
	found  member.Member
	error  error
	userID string
}

func (m *stubMembers) FindByUserID(_ context.Context, userID string) (member.Member, error) {
	m.userID = userID
	return m.found, m.error
}

type stubIssuer struct {
	token     string
	expiresAt time.Time
	error     error
	found     member.Member
}

func (i *stubIssuer) Issue(found member.Member) (string, time.Time, error) {
	i.found = found
	return i.token, i.expiresAt, i.error
}

func TestDiscordLoginIssuesAppTokenForRegisteredMember(t *testing.T) {
	verifier := &stubVerifier{user: DiscordUser{ID: "123", Username: "delta"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", Username: "delta", DisplayName: "Delta"}}
	expiresAt := time.Date(2026, 8, 14, 10, 15, 0, 0, time.UTC)
	issuer := &stubIssuer{token: "app-token", expiresAt: expiresAt}
	recorder := request(NewHandler(verifier, members, issuer, testLogger()), http.MethodPost, "/api/v1/auth/discord", "Bearer discord-token")

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"access_token":"app-token"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if verifier.token != "discord-token" || members.userID != "123" || issuer.found.ID != 7 {
		t.Fatalf("flow = verifier token %q, member user %q, issuer member %#v", verifier.token, members.userID, issuer.found)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestDiscordLoginRejectsInvalidAndUnregisteredUsers(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		verifyErr  error
		memberErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "missing bearer", wantStatus: http.StatusUnauthorized, wantCode: "INVALID_DISCORD_TOKEN"},
		{name: "invalid Discord token", header: "Bearer bad", verifyErr: ErrInvalidDiscordToken, wantStatus: http.StatusUnauthorized, wantCode: "INVALID_DISCORD_TOKEN"},
		{name: "unregistered member", header: "Bearer valid", memberErr: member.ErrNotFound, wantStatus: http.StatusForbidden, wantCode: "MEMBER_NOT_REGISTERED"},
		{name: "Discord unavailable", header: "Bearer valid", verifyErr: ErrDiscordUnavailable, wantStatus: http.StatusBadGateway, wantCode: "DISCORD_UNAVAILABLE"},
		{name: "database unavailable", header: "Bearer valid", memberErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_ERROR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &stubVerifier{user: DiscordUser{ID: "123", Username: "delta"}, error: test.verifyErr}
			members := &stubMembers{error: test.memberErr}
			recorder := request(NewHandler(verifier, members, &stubIssuer{}, testLogger()), http.MethodPost, "/api/v1/auth/discord", test.header)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), test.wantCode) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHealthAndMethodRouting(t *testing.T) {
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, testLogger())
	health := request(handler, http.MethodGet, "/healthz", "")
	if health.Code != http.StatusOK || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health response = %d %s", health.Code, health.Body.String())
	}
	wrongMethod := request(handler, http.MethodGet, "/api/v1/auth/discord", "")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method response = %d", wrongMethod.Code)
	}
}

func request(handler http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", authorization)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
