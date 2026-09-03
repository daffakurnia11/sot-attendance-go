package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

const webhookSecret = "0123456789abcdef0123456789abcdef"

type fakeServerLogStore struct {
	result serverlog.AcceptedResult
	err    error
	calls  int
	last   serverlog.ValidEvent
}

func (s *fakeServerLogStore) Store(_ context.Context, event serverlog.ValidEvent) (serverlog.AcceptedResult, error) {
	s.calls++
	s.last = event
	if s.err != nil {
		return serverlog.AcceptedResult{}, s.err
	}
	return s.result, nil
}

func webhookHandler(t *testing.T, store serverLogStore) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := serverlog.NewAuthenticator(webhookSecret, nil)
	webhook := NewServerLogWebhook(store, auth)
	return NewHandlerWithWebhook(nil, nil, nil, nil, nil, nil, logger, nil, nil, nil, webhook)
}

func text(value string) *string { return &value }

func webhookBody(t *testing.T, mutate func(*serverlog.Event)) []byte {
	t.Helper()
	ping := 34
	event := serverlog.Event{
		Player: serverlog.Player{
			ServerID: 142,
			Name:     "SOT - Ayvix",
			Username: "Kenji Nakamura",
			CID:      "CID-1024",
			Identifiers: serverlog.Identifiers{
				License:  "license:kenji-smoketest",
				Discord:  "406954574998536202",
				FiveM:    "fivem:123123",
				SteamHex: "steam:123123123",
			},
			Ping: &ping,
		},
		// Freshness is checked against this value now, so it has to be current.
		Event: serverlog.EventDetail{Type: "connecting", Timestamp: time.Now().UTC().Format(time.RFC3339)},
	}
	if mutate != nil {
		mutate(&event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return encoded
}

func signedRequest(body []byte, tweak func(*http.Request)) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/server-logs", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-SOT-Contract-Version", serverlog.ContractVersion)
	request.Header.Set("X-SOT-Secret", webhookSecret)
	if tweak != nil {
		tweak(request)
	}
	return request
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var payload struct {
		Error map[string]string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, recorder.Body.String())
	}
	return payload.Error
}

func TestServerLogWebhookAcceptsEachStatus(t *testing.T) {
	for _, eventType := range []string{"connecting", "connected", "disconnected"} {
		store := &fakeServerLogStore{result: serverlog.AcceptedResult{MatchedMember: true, SessionID: "s-1"}}
		body := webhookBody(t, func(event *serverlog.Event) {
			event.Event.Type = eventType
			if eventType == "disconnected" {
				event.Event.Reason = text("Connection Timed Out")
			}
		})
		recorder := httptest.NewRecorder()
		webhookHandler(t, store).ServeHTTP(recorder, signedRequest(body, nil))

		if recorder.Code != http.StatusAccepted {
			t.Fatalf("%s status = %d, want 202 (%s)", eventType, recorder.Code, recorder.Body.String())
		}
		var payload struct {
			SessionID     string `json:"session_id"`
			Accepted      bool   `json:"accepted"`
			Duplicate     bool   `json:"duplicate"`
			MatchedMember bool   `json:"matched_member"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if !payload.Accepted || payload.Duplicate || !payload.MatchedMember {
			t.Fatalf("%s body = %#v", eventType, payload)
		}
		if recorder.Header().Get("X-SOT-Request-Id") == "" {
			t.Fatalf("%s missing X-SOT-Request-Id", eventType)
		}
		if store.calls != 1 {
			t.Fatalf("%s store calls = %d, want 1", eventType, store.calls)
		}
	}
}

// The exact request body reaches the store, where it is both persisted for
// debugging and used as the idempotency key.
func TestServerLogWebhookPassesRawBodyToStore(t *testing.T) {
	store := &fakeServerLogStore{}
	body := webhookBody(t, nil)

	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(body, nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", recorder.Code, recorder.Body.String())
	}
	if string(store.last.Payload) != string(body) {
		t.Fatalf("stored payload = %q, want %q", store.last.Payload, body)
	}
}

func TestServerLogWebhookReportsDuplicate(t *testing.T) {
	store := &fakeServerLogStore{result: serverlog.AcceptedResult{Duplicate: true}}
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(webhookBody(t, nil), nil))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"duplicate":true`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestServerLogWebhookAcceptsUnmatchedMember(t *testing.T) {
	store := &fakeServerLogStore{result: serverlog.AcceptedResult{MatchedMember: false}}
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(webhookBody(t, nil), nil))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"matched_member":false`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestServerLogWebhookErrorCodes(t *testing.T) {
	body := webhookBody(t, nil)
	oversized := append([]byte(`{"pad":"`), append([]byte(strings.Repeat("x", serverlog.MaxBodyBytes+64)), []byte(`"}`)...)...)

	for _, test := range []struct {
		name     string
		body     []byte
		tweak    func(*http.Request)
		wantCode int
		wantErr  string
	}{
		{"wrong content type", body, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"},
		{"missing content type", body, func(r *http.Request) { r.Header.Del("Content-Type") }, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE"},
		{"wrong secret", body, func(r *http.Request) { r.Header.Set("X-SOT-Secret", strings.Repeat("0", 32)) }, http.StatusUnauthorized, "INVALID_SECRET"},
		{"missing secret", body, func(r *http.Request) { r.Header.Del("X-SOT-Secret") }, http.StatusUnauthorized, "INVALID_SECRET"},
		{"oversized body", oversized, nil, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeServerLogStore{}
			recorder := httptest.NewRecorder()
			webhookHandler(t, store).ServeHTTP(recorder, signedRequest(test.body, test.tweak))

			if recorder.Code != test.wantCode {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, test.wantCode, recorder.Body.String())
			}
			if got := decodeError(t, recorder)["code"]; got != test.wantErr {
				t.Fatalf("error code = %q, want %q", got, test.wantErr)
			}
			if decodeError(t, recorder)["request_id"] == "" {
				t.Fatal("error envelope missing request_id")
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0", store.calls)
			}
		})
	}
}

// A stale event.timestamp is the replay guard, in place of a timestamp header.
func TestServerLogWebhookRejectsStaleTimestamp(t *testing.T) {
	for _, offset := range []time.Duration{-serverlog.TimestampSkew - time.Minute, serverlog.TimestampSkew + time.Minute} {
		store := &fakeServerLogStore{}
		body := webhookBody(t, func(event *serverlog.Event) {
			event.Event.Timestamp = time.Now().UTC().Add(offset).Format(time.RFC3339)
		})
		recorder := httptest.NewRecorder()
		webhookHandler(t, store).ServeHTTP(recorder, signedRequest(body, nil))

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("offset %v status = %d, want 401 (%s)", offset, recorder.Code, recorder.Body.String())
		}
		if got := decodeError(t, recorder)["code"]; got != "EXPIRED_TIMESTAMP" {
			t.Fatalf("offset %v error code = %q, want EXPIRED_TIMESTAMP", offset, got)
		}
		if store.calls != 0 {
			t.Fatalf("stale event reached the store")
		}
	}
}

func TestServerLogWebhookRejectsMalformedJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"not json", []byte(`nope`)},
		{"unknown field", []byte(`{"player":{},"surprise":1}`)},
		{"trailing object", append(webhookBody(t, nil), []byte(`{"extra":1}`)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeServerLogStore{}
			recorder := httptest.NewRecorder()
			webhookHandler(t, store).ServeHTTP(recorder, signedRequest(test.body, nil))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", recorder.Code, recorder.Body.String())
			}
			if got := decodeError(t, recorder)["code"]; got != "INVALID_JSON" {
				t.Fatalf("error code = %q, want INVALID_JSON", got)
			}
		})
	}
}

func TestServerLogWebhookContractVersion(t *testing.T) {
	store := &fakeServerLogStore{}
	for _, version := range []string{"2.0", "1.2", ""} {
		recorder := httptest.NewRecorder()
		webhookHandler(t, store).ServeHTTP(recorder, signedRequest(webhookBody(t, nil), func(r *http.Request) {
			r.Header.Set("X-SOT-Contract-Version", version)
		}))
		if got := decodeError(t, recorder)["code"]; got != "UNSUPPORTED_CONTRACT_VERSION" {
			t.Fatalf("version %q error code = %q (%d)", version, got, recorder.Code)
		}
	}
}

func TestServerLogWebhookRejectsInvalidEvent(t *testing.T) {
	store := &fakeServerLogStore{}
	body := webhookBody(t, func(event *serverlog.Event) { event.Player.Identifiers.License = "" })
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(body, nil))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", recorder.Code, recorder.Body.String())
	}
	if got := decodeError(t, recorder)["code"]; got != "INVALID_EVENT" {
		t.Fatalf("error code = %q, want INVALID_EVENT", got)
	}
}

func TestServerLogWebhookRepositoryFailureIsSafe(t *testing.T) {
	store := &fakeServerLogStore{err: errors.New("connection refused to postgres://sot:hunter2@db")}
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(webhookBody(t, nil), nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if got := decodeError(t, recorder)["code"]; got != "INTERNAL_ERROR" {
		t.Fatalf("error code = %q, want INTERNAL_ERROR", got)
	}
	if strings.Contains(recorder.Body.String(), "hunter2") || strings.Contains(recorder.Body.String(), "postgres://") {
		t.Fatalf("500 body leaked internal detail: %s", recorder.Body.String())
	}
}

// The HMAC is the sole authentication. A member bearer token is neither
// required nor consulted.
func TestServerLogWebhookNeedsNoMemberToken(t *testing.T) {
	store := &fakeServerLogStore{}
	request := signedRequest(webhookBody(t, nil), nil)
	if request.Header.Get("Authorization") != "" {
		t.Fatal("test request unexpectedly carries Authorization")
	}
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 without any bearer token", recorder.Code)
	}
}

// No response may echo the shared secret, the signature, or a full license.
func TestServerLogWebhookResponsesLeakNothing(t *testing.T) {
	store := &fakeServerLogStore{}
	body := webhookBody(t, func(event *serverlog.Event) {
		event.Player.Identifiers.License = "license:leaky-secret-identifier"
		event.Player.Name = ""
	})
	recorder := httptest.NewRecorder()
	webhookHandler(t, store).ServeHTTP(recorder, signedRequest(body, nil))

	rendered := recorder.Body.String()
	for _, forbidden := range []string{webhookSecret, "leaky-secret-identifier"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, rendered)
		}
	}
}

// A webhook built by struct literal without a Limiter must report 503, not
// panic on a nil mutex.
func TestServerLogWebhookUnavailableWithoutLimiter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	webhook := &ServerLogWebhook{
		Store: &fakeServerLogStore{},
		Auth:  serverlog.NewAuthenticator(webhookSecret, nil),
	}
	handler := NewHandlerWithWebhook(nil, nil, nil, nil, nil, nil, logger, nil, nil, nil, webhook)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, signedRequest(webhookBody(t, nil), nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestServerLogWebhookUnavailableWithoutStore(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandlerWithWebhook(nil, nil, nil, nil, nil, nil, logger, nil, nil, nil, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, signedRequest(webhookBody(t, nil), nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestServerLogWebhookRateLimits(t *testing.T) {
	store := &fakeServerLogStore{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := serverlog.NewAuthenticator(webhookSecret, nil)
	webhook := &ServerLogWebhook{Store: store, Auth: auth, Limiter: newRateLimiter(1, 1, nil)}
	handler := NewHandlerWithWebhook(nil, nil, nil, nil, nil, nil, logger, nil, nil, nil, webhook)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, signedRequest(webhookBody(t, nil), nil))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202", first.Code)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, signedRequest(webhookBody(t, nil), nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
	if got := decodeError(t, second)["code"]; got != "RATE_LIMITED" {
		t.Fatalf("error code = %q, want RATE_LIMITED", got)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("429 missing Retry-After")
	}
}
