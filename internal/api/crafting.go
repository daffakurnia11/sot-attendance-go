package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/daffakurniawan/sot-discord-bot/internal/crafting"
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
	if len(payload.Recipes) < 1 || len(payload.Recipes) > crafting.MaxRecipes {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_REQUEST", "Recipe count must be between 1 and 20")
		return
	}
	seen := make(map[string]struct{}, len(payload.Recipes))
	calculations := make([]crafting.Calculation, 0, len(payload.Recipes))
	for _, item := range payload.Recipes {
		item.WeaponCode = strings.TrimSpace(item.WeaponCode)
		if item.WeaponCode == "" || len(item.WeaponCode) > 80 || item.Quantity < 1 || item.Quantity > crafting.MaxQuantity {
			writeError(response, http.StatusUnprocessableEntity, "INVALID_CRAFTING_REQUEST", "Every weapon is required and quantity must be between 1 and 10000")
			return
		}
		if _, exists := seen[item.WeaponCode]; exists {
			writeError(response, http.StatusUnprocessableEntity, "DUPLICATE_CRAFTING_RECIPE", "Each weapon recipe may only be selected once")
			return
		}
		seen[item.WeaponCode] = struct{}{}
		recipe, err := h.crafting.Get(request.Context(), item.WeaponCode)
		if errors.Is(err, crafting.ErrNotFound) {
			writeError(response, http.StatusNotFound, "CRAFTING_RECIPE_NOT_FOUND", "Crafting recipe was not found")
			return
		}
		if err != nil {
			h.logger.Error("load batch crafting recipe", "member_id", claims.MemberID, "weapon_code", item.WeaponCode, "error", err)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipe could not be loaded")
			return
		}
		calculation, err := crafting.Calculate(recipe, item.Quantity)
		if err != nil {
			h.logger.Error("calculate batch crafting recipe", "member_id", claims.MemberID, "weapon_code", item.WeaponCode, "quantity", item.Quantity, "error", err)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipe could not be calculated")
			return
		}
		calculations = append(calculations, calculation)
	}
	result, err := crafting.Combine(calculations)
	if err != nil {
		h.logger.Error("combine crafting recipes", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Crafting recipes could not be combined")
		return
	}
	h.logger.Info("batch crafting recipes calculated", "member_id", claims.MemberID, "recipe_count", len(calculations), "weapon_quantity", result.TotalRequestedQuantity)
	writeJSON(response, http.StatusOK, result)
}
