package api

import (
	"context"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"net/http"
	"testing"
)

type stubOrderLog struct {
	stubBusinessCatalog
	before int64
	called bool
}

func (s *stubOrderLog) ListOrders(_ context.Context, before int64, kind string) (business.OrderLog, error) {
	s.before = before
	s.called = true
	return business.OrderLog{Orders: []business.OrderLogEntry{}}, nil
}
func TestOrderLogAccessAndCursor(t *testing.T) {
	for _, tc := range []struct {
		path, token string
		status      int
		called      bool
		before      int64
	}{
		{"/api/v1/order-log", "", 401, false, 0},
		{"/api/v1/order-log", "Bearer token", 200, true, 0},
		{"/api/v1/order-log?before=42", "Bearer token", 200, true, 42},
		{"/api/v1/order-log?before=-1", "Bearer token", 400, false, 0},
		{"/api/v1/order-log?kind=invalid", "Bearer token", 400, false, 0},
		{"/api/v1/order-log?kind=internal", "Bearer token", 200, true, 0},
		{"/api/v1/order-log?before=oops", "Bearer token", 400, false, 0},
	} {
		store := &stubOrderLog{}
		h := NewHandler(Deps{Tokens: stubTokens{claims: appauth.Claims{MemberID: 7}}, Business: store, Logger: testLogger()})
		response := request(h, http.MethodGet, tc.path, tc.token)
		if response.Code != tc.status || store.called != tc.called || store.before != tc.before {
			t.Fatalf("%s: status=%d called=%v before=%d", tc.path, response.Code, store.called, store.before)
		}
	}
}
