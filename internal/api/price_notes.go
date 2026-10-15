package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"io"
	"net/http"
)

func (h *Handler) priceNotes(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if h.business == nil {
		writeError(w, http.StatusServiceUnavailable, "PRICE_NOTES_UNAVAILABLE", "Price notes are unavailable")
		return
	}
	entries, err := h.business.List(r.Context())
	if err != nil {
		h.logger.Error("load price notes", "member_id", claims.MemberID, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Price notes could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries})
}

type businessCatalogWriter interface {
	Update(context.Context, business.Update, int64) (int64, error)
}

func (h *Handler) updatePriceNote(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.admin(w, r, "price management")
	if !ok {
		return
	}
	store, ok := h.business.(businessCatalogWriter)
	if !ok {
		writeError(w, 503, "PRICE_NOTES_UNAVAILABLE", "Price management is unavailable")
		return
	}
	var u business.Update
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&u); err != nil || !u.Valid() {
		writeError(w, 400, "INVALID_PRICE", "Invalid price, unit or notes")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, 400, "INVALID_PRICE", "Expected one JSON object")
		return
	}
	version, err := store.Update(r.Context(), u, claims.MemberID)
	if err != nil {
		switch {
		case errors.Is(err, business.ErrInvalid):
			writeError(w, 400, "INVALID_PRICE", err.Error())
		case errors.Is(err, business.ErrConflict), errors.Is(err, business.ErrDuplicate):
			writeError(w, 409, "PRICE_CONFLICT", err.Error())
		case errors.Is(err, business.ErrNotFound):
			writeError(w, 404, "PRICE_NOT_FOUND", err.Error())
		default:
			h.logger.Error("update business price", "member_id", claims.MemberID, "entry_id", u.ID, "direction", u.Direction, "error", err)
			writeError(w, 500, "INTERNAL_ERROR", "Price could not be saved")
		}
		return
	}
	h.logger.Info("business price saved", "member_id", claims.MemberID, "entry_id", u.ID, "direction", u.Direction, "version", version)
	writeJSON(w, 200, map[string]any{"version": version})
}
