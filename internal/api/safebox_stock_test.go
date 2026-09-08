package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

type stubSafeboxStock struct {
	items          []stock.Item
	transaction    stock.Transaction
	movementBatch  stock.MovementBatch
	alreadyApplied bool
	err            error
}

func (s *stubSafeboxStock) ApplyMovements(_ context.Context, batch stock.MovementBatch) (bool, error) {
	s.movementBatch = batch
	return s.err == nil && !s.alreadyApplied, s.err
}

func requestWithBody(handler http.Handler, method, path, authorization, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (s *stubSafeboxStock) List(context.Context) ([]stock.Item, error) { return s.items, nil }
func (s *stubSafeboxStock) Transact(_ context.Context, transaction stock.Transaction) error {
	s.transaction = transaction
	return s.err
}

func TestSafeboxStockTransactionRequiresAdminAndRecordsActor(t *testing.T) {
	store := &stubSafeboxStock{}
	members := &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: true}}
	handler := NewHandlerWithWebhook(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, nil, nil, nil, store)

	response := requestWithBody(handler, http.MethodPost, "/api/v1/safebox-stock/transactions", "Bearer app-token", `{"safebox":"public","action":"deposit","reason":"restock","idempotency_key":"request-1","items":[{"item_key":"iron","quantity":2}]}`)
	if response.Code != http.StatusNoContent || store.transaction.ActorMemberID != 7 || store.transaction.Items[0].ItemKey != "iron" || response.Body.Len() != 0 {
		t.Fatalf("response = %d %s, transaction = %+v", response.Code, response.Body.String(), store.transaction)
	}

	members.found.IsAdmin = false
	response = requestWithBody(handler, http.MethodPost, "/api/v1/safebox-stock/transactions", "Bearer app-token", `{"safebox":"public","action":"deposit","reason":"restock","idempotency_key":"request-2","items":[{"item_key":"iron","quantity":2}]}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin response = %d %s", response.Code, response.Body.String())
	}
}

func TestSafeboxStockTransactionMapsInsufficientStock(t *testing.T) {
	store := &stubSafeboxStock{err: stock.ErrInsufficientStock}
	members := &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: true}}
	handler := NewHandlerWithWebhook(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, nil, nil, nil, store)

	response := requestWithBody(handler, http.MethodPost, "/api/v1/safebox-stock/transactions", "Bearer app-token", `{"safebox":"public","action":"withdraw","reason":"usage","idempotency_key":"request-3","items":[{"item_key":"iron","quantity":99}]}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "INSUFFICIENT_SAFEBOX_STOCK") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestSafeboxStockTransactionMapsIdempotencyConflict(t *testing.T) {
	store := &stubSafeboxStock{err: stock.ErrIdempotencyConflict}
	members := &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: true}}
	handler := NewHandlerWithWebhook(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, nil, nil, nil, store)

	response := requestWithBody(handler, http.MethodPost, "/api/v1/safebox-stock/transactions", "Bearer app-token", `{"safebox":"public","action":"deposit","reason":"restock","idempotency_key":"request-4","items":[{"item_key":"iron","quantity":2}]}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "SAFEBOX_IDEMPOTENCY_CONFLICT") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestSafeboxStockTransactionRejectsUnknownFields(t *testing.T) {
	store := &stubSafeboxStock{err: errors.New("must not be called")}
	members := &stubMembers{found: member.Member{ID: 7, DiscordUserID: "123", IsAdmin: true}}
	handler := NewHandlerWithWebhook(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, nil, nil, nil, store)

	response := requestWithBody(handler, http.MethodPost, "/api/v1/safebox-stock/transactions", "Bearer app-token", `{"safebox":"public","action":"deposit","reason":"restock","items":[],"extra":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
