package api

import (
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"net/http"
	"strconv"
)

type orderLogReader interface {
	ListOrders(context.Context, int64, string) (business.OrderLog, error)
}

func (h *Handler) orderLog(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	before := int64(0)
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			writeError(w, 400, "INVALID_CURSOR", "Invalid order cursor")
			return
		}
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "sale" && kind != "purchase" && kind != "internal" {
		writeError(w, 400, "INVALID_KIND", "Invalid order type")
		return
	}
	reader, ok := h.business.(orderLogReader)
	if !ok {
		writeError(w, 503, "ORDER_LOG_UNAVAILABLE", "Order log unavailable")
		return
	}
	result, err := reader.ListOrders(r.Context(), before, kind)
	if err != nil {
		h.logger.Error("load order log", "member_id", claims.MemberID, "error", err)
		writeError(w, 500, "INTERNAL_ERROR", "Order log could not be loaded")
		return
	}
	writeJSON(w, 200, result)
}
