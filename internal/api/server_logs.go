package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

type serverLogStore interface {
	Store(context.Context, serverlog.ValidEvent) (serverlog.AcceptedResult, error)
}

// ServerLogWebhook carries the dependencies for POST /api/v1/webhooks/server-logs.
type ServerLogWebhook struct {
	Store   serverLogStore
	Auth    *serverlog.Authenticator
	Limiter *rateLimiter
}

// Rate limits published in contract section 6.
const (
	serverLogRatePerSecond = 100
	serverLogBurst         = 500
)

// webhookRateLimitKey is the single bucket for this route. One shared secret
// means one sender, so the limit is per-endpoint rather than per-credential. It
// now also caps unauthenticated traffic, since the check runs before the body
// is read.
const webhookRateLimitKey = "server-logs"

// NewServerLogWebhook wires a store and verifier with the published limits.
func NewServerLogWebhook(store serverLogStore, auth *serverlog.Authenticator) *ServerLogWebhook {
	return &ServerLogWebhook{
		Store:   store,
		Auth:    auth,
		Limiter: newRateLimiter(serverLogRatePerSecond, serverLogBurst, nil),
	}
}

// serverLogWebhook ingests one FiveM player lifecycle event.
//
// The shared secret in X-SOT-Secret is the sole authentication for this route;
// there is no member JWT. Nothing in the body is read until it matches.
func (h *Handler) serverLogWebhook(response http.ResponseWriter, request *http.Request) {
	requestID := newRequestID()
	response.Header().Set("X-SOT-Request-Id", requestID)

	if h.serverLogs == nil || h.serverLogs.Store == nil || h.serverLogs.Auth == nil || h.serverLogs.Limiter == nil {
		writeWebhookError(response, http.StatusServiceUnavailable, "SERVER_LOGS_UNAVAILABLE", "Server log ingestion is unavailable", requestID)
		return
	}

	if !isJSONContentType(request.Header.Get("Content-Type")) {
		writeWebhookError(response, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json", requestID)
		return
	}

	// The secret is a plain header, so authenticate before reading anything.
	// An unauthenticated request now costs a map lookup instead of a 16 KiB
	// read plus a hash.
	if err := h.serverLogs.Auth.Authenticate(request.Header.Get("X-SOT-Secret")); err != nil {
		h.logger.Warn("server log invalid secret", "request_id", requestID)
		writeWebhookError(response, http.StatusUnauthorized, "INVALID_SECRET", "X-SOT-Secret is missing or does not match", requestID)
		return
	}

	if allowed, retryAfter := h.serverLogs.Limiter.allow(webhookRateLimitKey); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second)/time.Second)))
		writeWebhookError(response, http.StatusTooManyRequests, "RATE_LIMITED", "Rate limit exceeded", requestID)
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, serverlog.MaxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeWebhookError(response, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Request body must not exceed 16 KiB", requestID)
			return
		}
		writeWebhookError(response, http.StatusBadRequest, "INVALID_JSON", "Request body could not be read", requestID)
		return
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var event serverlog.Event
	if err := decoder.Decode(&event); err != nil {
		writeWebhookError(response, http.StatusBadRequest, "INVALID_JSON", "Request body is not a valid event object", requestID)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeWebhookError(response, http.StatusBadRequest, "INVALID_JSON", "Request body must contain one JSON object", requestID)
		return
	}

	if request.Header.Get("X-SOT-Contract-Version") != serverlog.ContractVersion {
		writeWebhookError(response, http.StatusBadRequest, "UNSUPPORTED_CONTRACT_VERSION", "X-SOT-Contract-Version is not served", requestID)
		return
	}

	// The body itself is stored and doubles as the idempotency key, so a retry
	// lands on the same row without the sender generating an event id.
	valid, err := serverlog.Validate(body, event)
	if err != nil {
		var validation *serverlog.ValidationError
		if errors.As(err, &validation) {
			writeWebhookError(response, http.StatusUnprocessableEntity, "INVALID_EVENT", validation.Message, requestID)
			return
		}
		writeWebhookError(response, http.StatusUnprocessableEntity, "INVALID_EVENT", "Event failed validation", requestID)
		return
	}

	// event.timestamp bounds accidental redelivery and stale sender queues. It
	// is not a replay defence: anyone holding the secret can mint a fresh time.
	if !h.serverLogs.Auth.Fresh(valid.OccurredAt) {
		writeWebhookError(response, http.StatusUnauthorized, "EXPIRED_TIMESTAMP", "event.timestamp is outside the allowed window", requestID)
		return
	}

	result, err := h.serverLogs.Store.Store(request.Context(), valid)
	if err != nil {
		h.logger.Error("store server log", "request_id", requestID, "status", valid.Status, "error", err)
		writeWebhookError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Event could not be stored", requestID)
		return
	}

	fields := []any{
		"request_id", requestID,
		"session_id", result.SessionID,
		"status", valid.Status,
		"duplicate", result.Duplicate,
		"matched_member", result.MatchedMember,
	}
	if result.MemberID != nil {
		fields = append(fields, "member_id", *result.MemberID)
	}
	h.logger.Info("server log accepted", fields...)
	if len(result.IdentityMismatch) > 0 {
		// The event is stored either way. This only tells an operator that a
		// license reported identifiers that disagree with what is on file.
		h.logger.Warn("server log identity mismatch",
			"request_id", requestID,
			"server_member_id", result.ServerMemberID,
			"fields", result.IdentityMismatch)
	}

	writeJSON(response, http.StatusAccepted, map[string]any{
		"session_id":     result.SessionID,
		"accepted":       true,
		"duplicate":      result.Duplicate,
		"matched_member": result.MatchedMember,
	})
}

func valueContainsNull(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if valueContainsNull(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if valueContainsNull(child) {
				return true
			}
		}
	}
	return false
}

func writeWebhookError(response http.ResponseWriter, status int, code, message, requestID string) {
	writeJSON(response, status, map[string]any{"error": map[string]string{
		"code":       code,
		"message":    message,
		"request_id": requestID,
	}})
}

func isJSONContentType(value string) bool {
	if value == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}
