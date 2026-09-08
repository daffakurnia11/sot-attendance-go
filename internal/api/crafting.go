package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/daffakurniawan/sot-discord-bot/internal/crafting"
	"github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

func (h *Handler) craftingRecipes(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	if h.crafting == nil {
		writeError(response, http.StatusServiceUnavailable, "CRAFTING_UNAVAILABLE", "Crafting recipes are unavailable")
		return
	}
	recipes, err := h.crafting.List(request.Context())
	if err != nil {
		h.logger.Error("list crafting recipes", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipes could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Recipes []crafting.RecipeSummary `json:"recipes"`
	}{Recipes: recipes})
}

func (h *Handler) calculateCrafting(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	if h.crafting == nil {
		writeError(response, http.StatusServiceUnavailable, "CRAFTING_UNAVAILABLE", "Crafting recipes are unavailable")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		WeaponCode string `json:"weapon_code"`
		Quantity   int64  `json:"quantity"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_CRAFTING_REQUEST", "Crafting payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_CRAFTING_REQUEST", "Crafting payload must contain one JSON object")
		return
	}
	payload.WeaponCode = strings.TrimSpace(payload.WeaponCode)
	if payload.WeaponCode == "" || len(payload.WeaponCode) > 80 || payload.Quantity < 1 || payload.Quantity > crafting.MaxQuantity {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_REQUEST", "Weapon is required and quantity must be between 1 and 10000")
		return
	}
	recipe, err := h.crafting.Get(request.Context(), payload.WeaponCode)
	if errors.Is(err, crafting.ErrNotFound) {
		writeError(response, http.StatusNotFound, "CRAFTING_RECIPE_NOT_FOUND", "Crafting recipe was not found")
		return
	}
	if err != nil {
		h.logger.Error("load crafting recipe", "member_id", claims.MemberID, "weapon_code", payload.WeaponCode, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipe could not be loaded")
		return
	}
	calculation, err := crafting.Calculate(recipe, payload.Quantity)
	if err != nil {
		h.logger.Error("calculate crafting recipe", "member_id", claims.MemberID, "weapon_code", payload.WeaponCode, "quantity", payload.Quantity, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipe could not be calculated")
		return
	}
	h.logger.Info("crafting recipe calculated", "member_id", claims.MemberID, "weapon_code", payload.WeaponCode, "quantity", payload.Quantity)
	writeJSON(response, http.StatusOK, calculation)
}

func (h *Handler) calculateCraftingBatch(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	if h.crafting == nil {
		writeError(response, http.StatusServiceUnavailable, "CRAFTING_UNAVAILABLE", "Crafting recipes are unavailable")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8192)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		Recipes []crafting.BatchItem `json:"recipes"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_CRAFTING_REQUEST", "Crafting payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_CRAFTING_REQUEST", "Crafting payload must contain one JSON object")
		return
	}
	result, err := crafting.CalculateBatch(request.Context(), h.crafting, payload.Recipes)
	if errors.Is(err, crafting.ErrInvalidBatch) {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_REQUEST", "Every weapon is required, recipe count must be between 1 and 20, and quantity must be between 1 and 10000")
		return
	}
	if errors.Is(err, crafting.ErrDuplicateRecipe) {
		writeError(response, http.StatusUnprocessableEntity, "DUPLICATE_CRAFTING_RECIPE", "Each weapon recipe may only be selected once")
		return
	}
	if errors.Is(err, crafting.ErrNotFound) {
		writeError(response, http.StatusNotFound, "CRAFTING_RECIPE_NOT_FOUND", "Crafting recipe was not found")
		return
	}
	if err != nil {
		h.logger.Error("calculate batch crafting recipes", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipes could not be combined")
		return
	}
	if h.stock != nil {
		stockItems, stockErr := h.stock.List(request.Context())
		if stockErr != nil {
			h.logger.Error("load crafting stock availability", "member_id", claims.MemberID, "error", stockErr)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting stock availability could not be loaded")
			return
		}
		available := make(map[string]crafting.StockAvailability)
		for _, item := range stockItems {
			quantity := available[item.ItemKey]
			if item.Safebox == "public" {
				quantity.PublicQuantity = int64(item.Quantity)
			} else if item.Safebox == "boss" {
				quantity.BossQuantity = int64(item.Quantity)
			}
			available[item.ItemKey] = quantity
		}
		crafting.ApplyStockAvailability(&result, available)
	}
	h.logger.Info("batch crafting recipes calculated", "member_id", claims.MemberID, "recipe_count", len(result.Recipes), "weapon_quantity", result.TotalRequestedQuantity)
	writeJSON(response, http.StatusOK, result)
}

func (h *Handler) storeCraftingStock(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.admin(response, request, "store crafting stock")
	if !ok {
		return
	}
	writer, ok := h.stock.(craftingStockWriter)
	if !ok || h.crafting == nil {
		writeError(response, http.StatusServiceUnavailable, "CRAFTING_STOCK_UNAVAILABLE", "Crafting stock is unavailable")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 8192)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		Recipes        []crafting.BatchItem `json:"recipes"`
		Destination    string               `json:"destination"`
		IdempotencyKey string               `json:"idempotency_key"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_CRAFTING_STOCK_REQUEST", "Crafting stock payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || (payload.Destination != "public" && payload.Destination != "boss") || strings.TrimSpace(payload.IdempotencyKey) == "" || len(payload.IdempotencyKey) > 128 {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_STOCK_REQUEST", "Crafting stock request is invalid")
		return
	}
	result, err := crafting.CalculateBatch(request.Context(), h.crafting, payload.Recipes)
	if err != nil {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_STOCK_REQUEST", "Crafting recipes could not be calculated")
		return
	}
	items, err := h.stock.List(request.Context())
	if err != nil {
		h.logger.Error("load stock for crafting storage", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting stock could not be loaded")
		return
	}
	available := make(map[string]map[string]int64)
	itemNames := make(map[string]string)
	for _, item := range items {
		if available[item.ItemKey] == nil {
			available[item.ItemKey] = make(map[string]int64)
		}
		available[item.ItemKey][item.Safebox] = int64(item.Quantity)
		itemNames[item.ItemKey] = item.Name
	}
	movements := make([]stock.Movement, 0, len(result.Ingredients)*2+len(result.Recipes))
	for _, ingredient := range result.Ingredients {
		key := crafting.StockItemKey(ingredient)
		needed := ingredient.TotalQuantity
		public := min(needed, available[key]["public"])
		if public > 0 {
			movements = append(movements, stock.Movement{Safebox: "public", ItemKey: key, Action: stock.ActionWithdraw, Quantity: int32(public)})
		}
		needed -= public
		boss := min(needed, available[key]["boss"])
		if boss > 0 {
			movements = append(movements, stock.Movement{Safebox: "boss", ItemKey: key, Action: stock.ActionWithdraw, Quantity: int32(boss)})
		}
		needed -= boss
		if needed > 0 {
			writeError(response, http.StatusConflict, "INSUFFICIENT_SAFEBOX_STOCK", "Crafting materials exceed available stash stock")
			return
		}
	}
	for _, recipe := range result.Recipes {
		movements = append(movements, stock.Movement{Safebox: payload.Destination, ItemKey: recipe.WeaponCode, Action: stock.ActionDeposit, Quantity: int32(recipe.RequestedQuantity)})
	}
	applied, err := writer.ApplyMovements(request.Context(), stock.MovementBatch{Reason: "Crafting calculator stock update", ActorMemberID: claims.MemberID, IdempotencyKey: payload.IdempotencyKey, Movements: movements})
	if err != nil {
		switch {
		case errors.Is(err, stock.ErrInsufficientStock):
			writeError(response, http.StatusConflict, "INSUFFICIENT_SAFEBOX_STOCK", "Crafting materials exceed available stash stock")
		case errors.Is(err, stock.ErrQuantityOverflow):
			writeError(response, http.StatusConflict, "SAFEBOX_STOCK_OVERFLOW", "The stored stock quantity is too large")
		case errors.Is(err, stock.ErrItemNotFound):
			writeError(response, http.StatusConflict, "SAFEBOX_ITEM_NOT_FOUND", "A crafting item is no longer available in the stash catalog")
		case errors.Is(err, stock.ErrIdempotencyConflict):
			writeError(response, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This stock request key was already used for different data")
		case errors.Is(err, stock.ErrInvalidTransaction):
			writeError(response, http.StatusUnprocessableEntity, "INVALID_STOCK_TRANSACTION", "The crafting stock transaction is invalid")
		default:
			h.logger.Error("store crafting stock", "member_id", claims.MemberID, "error", err)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting stock could not be stored")
		}
		return
	}
	if applied && h.stockNotify != nil {
		if err := h.stockNotify.Notify(request.Context(), claims.DiscordUserID, movements, itemNames); err != nil {
			h.logger.Error("send crafting stock Discord embeds", "member_id", claims.MemberID, "error", err)
		}
	}
	h.logger.Info("crafting stock stored", "member_id", claims.MemberID, "destination", payload.Destination, "recipe_count", len(result.Recipes))
	response.WriteHeader(http.StatusNoContent)
}
