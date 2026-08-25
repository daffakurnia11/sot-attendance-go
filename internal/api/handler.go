package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	attendancehistory "github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/crafting"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"github.com/daffakurniawan/sot-discord-bot/internal/payslip"
	dbsettings "github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

type discordIdentityVerifier interface {
	Verify(context.Context, string) (DiscordUser, error)
}

type memberFinder interface {
	FindByUserID(context.Context, string) (member.Member, error)
}
type memberProfileUpdater interface {
	UpdateCharacterName(context.Context, int64, string) (member.Member, error)
}

type tokenIssuer interface {
	Issue(member.Member) (string, time.Time, error)
}

type tokenVerifier interface {
	Verify(string) (appauth.Claims, error)
}
type dashboardReader interface {
	Get(context.Context, int64) (dashboard.Snapshot, error)
	GetMemberRecords(context.Context, int64) (dashboard.MemberRecords, error)
}
type attendanceReader interface {
	GetMonthly(context.Context, int, time.Month, int) (attendancehistory.MonthlyReport, error)
}
type settingsStore interface {
	Load(context.Context) (dbsettings.Values, error)
	Update(context.Context, dbsettings.Values) (dbsettings.Values, error)
}
type Handler struct {
	verifier   discordIdentityVerifier
	members    memberFinder
	issuer     tokenIssuer
	tokens     tokenVerifier
	dashboard  dashboardReader
	attendance attendanceReader
	settings   settingsStore
	crafting   crafting.Store
	logger     *slog.Logger
}

func NewHandler(verifier discordIdentityVerifier, members memberFinder, issuer tokenIssuer, tokens tokenVerifier, dashboard dashboardReader, attendance attendanceReader, logger *slog.Logger, stores ...settingsStore) http.Handler {
	var settings settingsStore
	if len(stores) > 0 {
		settings = stores[0]
	}
	return newHandler(verifier, members, issuer, tokens, dashboard, attendance, logger, settings, nil)
}

func NewHandlerWithCrafting(verifier discordIdentityVerifier, members memberFinder, issuer tokenIssuer, tokens tokenVerifier, dashboard dashboardReader, attendance attendanceReader, logger *slog.Logger, settings settingsStore, recipes crafting.Store) http.Handler {
	return newHandler(verifier, members, issuer, tokens, dashboard, attendance, logger, settings, recipes)
}

func newHandler(verifier discordIdentityVerifier, members memberFinder, issuer tokenIssuer, tokens tokenVerifier, dashboard dashboardReader, attendance attendanceReader, logger *slog.Logger, settings settingsStore, recipes crafting.Store) http.Handler {
	handler := &Handler{verifier: verifier, members: members, issuer: issuer, tokens: tokens, dashboard: dashboard, attendance: attendance, settings: settings, crafting: recipes, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("POST /api/v1/auth/discord", handler.discordLogin)
	mux.HandleFunc("GET /api/v1/dashboard", handler.dashboardSnapshot)
	mux.HandleFunc("GET /api/v1/me/records", handler.memberRecords)
	mux.HandleFunc("GET /api/v1/attendance", handler.monthlyAttendance)
	mux.HandleFunc("GET /api/v1/attendance/me", handler.myMonthlyAttendance)
	mux.HandleFunc("GET /api/v1/payslips", handler.monthlyPayslips)
	mux.HandleFunc("GET /api/v1/settings", handler.getSettings)
	mux.HandleFunc("PATCH /api/v1/settings", handler.updateSettings)
	mux.HandleFunc("PATCH /api/v1/me/profile", handler.updateMyProfile)
	mux.HandleFunc("GET /api/v1/crafting/recipes", handler.craftingRecipes)
	mux.HandleFunc("POST /api/v1/crafting/calculate", handler.calculateCrafting)
	mux.HandleFunc("POST /api/v1/crafting/calculate-batch", handler.calculateCraftingBatch)
	return handler.logging(mux)
}

func (h *Handler) monthlyPayslips(response http.ResponseWriter, request *http.Request) {
	if _, ok := h.admin(response, request, "payslips"); !ok {
		return
	}
	claims, report, ok := h.loadMonthlyAttendance(response, request)
	if !ok {
		return
	}
	if h.settings == nil {
		writeError(response, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "Payslip settings are unavailable")
		return
	}
	values, err := h.settings.Load(request.Context())
	if err != nil {
		h.logger.Error("load payslip settings", "member_id", claims.MemberID, "month", report.Month, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Payslips could not be loaded")
		return
	}
	payslipReport, err := payslip.Calculate(report, values)
	if err != nil {
		h.logger.Error("calculate payslips", "member_id", claims.MemberID, "month", report.Month, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Payslips could not be calculated")
		return
	}
	writeJSON(response, http.StatusOK, payslipReport)
}

func (h *Handler) updateMyProfile(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	updater, ok := h.members.(memberProfileUpdater)
	if !ok {
		writeError(response, http.StatusServiceUnavailable, "PROFILE_UNAVAILABLE", "Profile is unavailable")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 1024)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload struct {
		CharacterName string `json:"character_name"`
	}
	if err := decoder.Decode(&payload); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_PROFILE", "Profile payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_PROFILE", "Profile payload must contain one JSON object")
		return
	}
	payload.CharacterName = strings.TrimSpace(payload.CharacterName)
	if payload.CharacterName == "" || len([]rune(payload.CharacterName)) > 80 || strings.ContainsAny(payload.CharacterName, "\r\n\x00") {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_CHARACTER_NAME", "Character name must contain 1 to 80 characters on one line")
		return
	}
	updated, err := updater.UpdateCharacterName(request.Context(), claims.MemberID, payload.CharacterName)
	if err != nil {
		h.logger.Error("update member profile", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Profile could not be updated")
		return
	}
	h.logger.Info("member profile updated", "member_id", claims.MemberID)
	writeJSON(response, http.StatusOK, map[string]string{"character_name": updated.CharacterName})
}

func (h *Handler) authenticated(response http.ResponseWriter, request *http.Request) (appauth.Claims, bool) {
	accessToken, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is required")
		return appauth.Claims{}, false
	}
	claims, err := h.tokens.Verify(accessToken)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is invalid or expired")
		return appauth.Claims{}, false
	}
	return claims, true
}

func (h *Handler) getSettings(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	if h.settings == nil {
		writeError(response, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "Settings are unavailable")
		return
	}
	currentMember, found := h.loadAuthenticatedMember(response, request, claims)
	if !found {
		return
	}
	values, err := h.settings.Load(request.Context())
	if err != nil {
		h.logger.Error("load settings", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Settings could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, struct {
		dbsettings.Values
		IsAdmin bool `json:"is_admin"`
	}{Values: values, IsAdmin: currentMember.IsAdmin})
}

func (h *Handler) updateSettings(response http.ResponseWriter, request *http.Request) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return
	}
	if h.settings == nil {
		writeError(response, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "Settings are unavailable")
		return
	}
	currentMember, found := h.loadAuthenticatedMember(response, request, claims)
	if !found {
		return
	}
	if !currentMember.IsAdmin {
		h.logger.Warn("non-admin settings update denied", "member_id", claims.MemberID)
		writeError(response, http.StatusForbidden, "ADMIN_REQUIRED", "Administrator role is required to update attendance settings")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, 4096)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var values dbsettings.Values
	if err := decoder.Decode(&values); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_SETTINGS", "Settings payload is invalid")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_SETTINGS", "Settings payload must contain one JSON object")
		return
	}
	validated, validationErr := dbsettings.Validate(values)
	if validationErr != nil {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_SETTINGS", validationErr.Error())
		return
	}
	validated.OfficeMoneyBalance, validationErr = dbsettings.ValidateOfficeMoneyBalance(values.OfficeMoneyBalance)
	if validationErr != nil {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_SETTINGS", validationErr.Error())
		return
	}
	validated.DirtyMoneyBalance, validationErr = dbsettings.ValidateDirtyMoneyBalance(values.DirtyMoneyBalance)
	if validationErr != nil {
		writeError(response, http.StatusUnprocessableEntity, "INVALID_SETTINGS", validationErr.Error())
		return
	}
	updated, err := h.settings.Update(request.Context(), validated)
	if err != nil {
		h.logger.Error("update settings", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Settings could not be updated")
		return
	}
	h.logger.Info("settings updated", "member_id", claims.MemberID)
	writeJSON(response, http.StatusOK, struct {
		dbsettings.Values
		IsAdmin bool `json:"is_admin"`
	}{Values: updated, IsAdmin: true})
}

// admin authenticates and then requires the administrator role.
//
// Guards the reports that cover the whole roster: every member's attendance
// grid and everyone's payout. A member's own figures stay open to them through
// /api/v1/attendance/me and /api/v1/me/records.
func (h *Handler) admin(response http.ResponseWriter, request *http.Request, action string) (appauth.Claims, bool) {
	claims, ok := h.authenticated(response, request)
	if !ok {
		return appauth.Claims{}, false
	}
	currentMember, found := h.loadAuthenticatedMember(response, request, claims)
	if !found {
		return appauth.Claims{}, false
	}
	if !currentMember.IsAdmin {
		h.logger.Warn("non-admin request denied", "member_id", claims.MemberID, "action", action)
		writeError(response, http.StatusForbidden, "ADMIN_REQUIRED", "Administrator role is required")
		return appauth.Claims{}, false
	}
	return claims, true
}

func (h *Handler) loadAuthenticatedMember(response http.ResponseWriter, request *http.Request, claims appauth.Claims) (member.Member, bool) {
	currentMember, err := h.members.FindByUserID(request.Context(), claims.DiscordUserID)
	if err != nil {
		h.logger.Error("load authenticated member", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Member permissions could not be loaded")
		return member.Member{}, false
	}
	return currentMember, true
}

func (h *Handler) memberRecords(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is required")
		return
	}
	claims, err := h.tokens.Verify(accessToken)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is invalid or expired")
		return
	}
	records, err := h.dashboard.GetMemberRecords(request.Context(), claims.MemberID)
	if err != nil {
		h.logger.Error("load member records", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Member records could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, records)
}

func (h *Handler) myMonthlyAttendance(response http.ResponseWriter, request *http.Request) {
	claims, report, ok := h.loadMonthlyAttendance(response, request)
	if !ok {
		return
	}
	personal := report.Members[:0]
	for _, record := range report.Members {
		if record.MemberID == claims.MemberID {
			personal = append(personal, record)
			break
		}
	}
	report.Members = personal
	report.TotalAttended = 0
	if len(personal) == 1 {
		report.TotalAttended = personal[0].TotalAttended
	}
	report.TotalOpportunities = len(personal) * len(report.AttendanceDays)
	writeJSON(response, http.StatusOK, report)
}

func (h *Handler) monthlyAttendance(response http.ResponseWriter, request *http.Request) {
	if _, ok := h.admin(response, request, "attendance"); !ok {
		return
	}
	_, report, ok := h.loadMonthlyAttendance(response, request)
	if !ok {
		return
	}
	writeJSON(response, http.StatusOK, report)
}

func (h *Handler) loadMonthlyAttendance(response http.ResponseWriter, request *http.Request) (appauth.Claims, attendancehistory.MonthlyReport, bool) {
	accessToken, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is required")
		return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
	}
	claims, err := h.tokens.Verify(accessToken)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is invalid or expired")
		return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
	}
	contractStartDay := 1
	if h.settings != nil {
		values, settingsErr := h.settings.Load(request.Context())
		if settingsErr != nil {
			h.logger.Error("load contract period setting", "member_id", claims.MemberID, "error", settingsErr)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Attendance period could not be loaded")
			return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
		}
		contractStartDay, err = strconv.Atoi(values.StartDateContract)
		if err != nil || contractStartDay < 1 || contractStartDay > 31 {
			h.logger.Error("invalid contract period setting", "member_id", claims.MemberID, "value", values.StartDateContract)
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Attendance period is invalid")
			return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
		}
	}
	year, month, err := requestedPeriod(request.URL.Query().Get("month"), time.Now(), contractStartDay)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_MONTH", "Month must use YYYY-MM format")
		return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
	}
	report, err := h.attendance.GetMonthly(request.Context(), year, month, contractStartDay)
	if err != nil {
		h.logger.Error("load monthly attendance", "member_id", claims.MemberID, "month", fmt.Sprintf("%04d-%02d", year, month), "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Attendance could not be loaded")
		return appauth.Claims{}, attendancehistory.MonthlyReport{}, false
	}
	return claims, report, true
}

func requestedPeriod(value string, now time.Time, contractStartDay int) (int, time.Month, error) {
	if value == "" {
		location, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			return 0, 0, err
		}
		now = now.In(location)
		lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, location).Day()
		boundaryDay := min(contractStartDay, lastDay)
		periodMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
		if now.Day() < boundaryDay {
			periodMonth = periodMonth.AddDate(0, -1, 0)
		}
		return periodMonth.Year(), periodMonth.Month(), nil
	}
	parsed, err := time.Parse("2006-01", value)
	if err != nil || parsed.Format("2006-01") != value {
		return 0, 0, errors.New("invalid month")
	}
	return parsed.Year(), parsed.Month(), nil
}

func (h *Handler) dashboardSnapshot(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request.Header.Get("Authorization"))
	if !ok {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is required")
		return
	}
	claims, err := h.tokens.Verify(accessToken)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "UNAUTHORIZED", "Member bearer token is invalid or expired")
		return
	}
	snapshot, err := h.dashboard.Get(request.Context(), claims.MemberID)
	if err != nil {
		h.logger.Error("load dashboard", "member_id", claims.MemberID, "error", err)
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "Dashboard could not be loaded")
		return
	}
	writeJSON(response, http.StatusOK, snapshot)
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
