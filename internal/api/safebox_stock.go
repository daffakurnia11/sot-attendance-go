package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

func (h *Handler) safeboxStock(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.admin(response, request, "safebox stock")
	if !ok {
		return
	}
	if h.stock == nil {
		writeError(response, http.StatusServiceUnavailable, "SAFEBOX_STOCK_UNAVAILABLE", "Safebox stock is unavailable")
		return
	}
	items, err := h.stock.List(request.Context())
	if err != nil {
		h.logger.Error("list safebox stock", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Safebox stock could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) safeboxStockTransactions(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.admin(response, request, "safebox stock transactions")
	if !ok {
		return
	}
	if h.stock == nil {
		writeError(response, http.StatusServiceUnavailable, "SAFEBOX_STOCK_UNAVAILABLE", "Safebox stock is unavailable")
		return
	}
	safebox := request.URL.Query().Get("safebox")
	entries, err := h.stock.ListTransactions(request.Context(), safebox)
	if err != nil {
		if errors.Is(err, stock.ErrInvalidTransaction) {
			writeError(response, http.StatusBadRequest, "INVALID_SAFEBOX", "Safebox must be public or boss")
			return
		}
		h.logger.Error("list safebox stock transactions", "member_id", claims.MemberID, "safebox", safebox, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Safebox transactions could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"transactions": entries})
}

func (h *Handler) safeboxStockTransaction(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.admin(response, request, "safebox stock transaction")
	if !ok {
		return
	}
	if h.stock == nil {
		writeError(response, http.StatusServiceUnavailable, "SAFEBOX_STOCK_UNAVAILABLE", "Safebox stock is unavailable")
		return
	}
	var transaction stock.Transaction
	decoder := json.NewDecoder(io.LimitReader(request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transaction); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_SAFEBOX_TRANSACTION", "Safebox transaction payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_SAFEBOX_TRANSACTION", "Safebox transaction payload must contain one JSON object")
		return
	}
	transaction.ActorMemberID = claims.MemberID
	if err := h.stock.Transact(request.Context(), transaction); err != nil {
		switch {
		case errors.Is(err, stock.ErrInvalidTransaction):
			writeError(response, http.StatusBadRequest, "INVALID_SAFEBOX_TRANSACTION", "Safebox transaction is invalid")
		case errors.Is(err, stock.ErrItemNotFound):
			writeError(response, http.StatusNotFound, "SAFEBOX_ITEM_NOT_FOUND", "Safebox item was not found")
		case errors.Is(err, stock.ErrInsufficientStock):
			writeError(response, http.StatusConflict, "INSUFFICIENT_SAFEBOX_STOCK", "Withdraw quantity exceeds available stock")
		case errors.Is(err, stock.ErrQuantityOverflow):
			writeError(response, http.StatusConflict, "SAFEBOX_STOCK_OVERFLOW", "Deposit quantity exceeds supported stock")
		case errors.Is(err, stock.ErrIdempotencyConflict):
			writeError(response, http.StatusConflict, "SAFEBOX_IDEMPOTENCY_CONFLICT", "Safebox transaction key was already used for different input")
		default:
			h.logger.Error("transact safebox stock", "member_id", claims.MemberID, "safebox", transaction.Safebox, "action", transaction.Action, "error", err)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Safebox transaction could not be completed")
		}
		return
	}
	h.logger.Info("safebox stock transaction completed", "member_id", claims.MemberID, "safebox", transaction.Safebox, "action", transaction.Action, "item_count", len(transaction.Items))
	response.WriteHeader(http.StatusNoContent)
}

var _ safeboxStockReader = (*stock.Repository)(nil)
