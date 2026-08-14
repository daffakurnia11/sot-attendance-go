package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

type discordIdentityVerifier interface {
	Verify(context.Context, string) (DiscordUser, error)
}

type memberFinder interface {
	FindByUserID(context.Context, string) (member.Member, error)
}

type tokenIssuer interface {
	Issue(member.Member) (string, time.Time, error)
}

type Handler struct {
	verifier discordIdentityVerifier
	members  memberFinder
	issuer   tokenIssuer
	logger   *slog.Logger
}

func NewHandler(verifier discordIdentityVerifier, members memberFinder, issuer tokenIssuer, logger *slog.Logger) http.Handler {
	handler := &Handler{verifier: verifier, members: members, issuer: issuer, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("POST /api/v1/auth/discord", handler.discordLogin)
	return handler.logging(mux)
}

func (h *Handler) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) discordLogin(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "INVALID_DISCORD_TOKEN", "Valid Discord bearer token is required")
		return
	}

	discordUser, err := h.verifier.Verify(request.Context(), accessToken)
	if err != nil {
		if errors.Is(err, ErrInvalidDiscordToken) {
			writeError(response, http.StatusUnauthorized, "INVALID_DISCORD_TOKEN", "Discord access token is invalid or expired")
			return
		}
		h.logger.Error("verify Discord identity", "error", err)
		writeError(response, http.StatusBadGateway, "DISCORD_UNAVAILABLE", "Discord identity could not be verified")
		return
	}

	found, err := h.members.FindByUserID(request.Context(), discordUser.ID)
	if err != nil {
		if errors.Is(err, member.ErrNotFound) {
			h.logger.Warn("unregistered Discord login denied", "discord_user_id", discordUser.ID)
			writeError(response, http.StatusForbidden, "MEMBER_NOT_REGISTERED", "Discord user is not registered as an SOT member")
			return
		}
		h.logger.Error("find login member", "discord_user_id", discordUser.ID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Login could not be completed")
		return
	}

	appToken, expiresAt, err := h.issuer.Issue(found)
	if err != nil {
		h.logger.Error("issue member access token", "member_id", found.ID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Login could not be completed")
		return
	}

	h.logger.Info("member login succeeded", "member_id", found.ID, "discord_user_id", found.UserID)
	writeJSON(response, http.StatusOK, map[string]any{
		"access_token": appToken,
		"token_type":   "Bearer",
		"expires_at":   expiresAt.UTC().Format(time.RFC3339),
		"member":       found,
	})
}

func (h *Handler) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(response, request)
		h.logger.Info("HTTP request", "method", request.Method, "path", request.URL.Path, "duration_ms", time.Since(startedAt).Milliseconds())
	})
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	returnToken := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""
	if !returnToken {
		return "", false
	}
	return parts[1], true
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

var _ tokenIssuer = (*appauth.Issuer)(nil)
