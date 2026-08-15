package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	attendancehistory "github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	dbsettings "github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

type stubVerifier struct {
	user  DiscordUser
	error error
	token string
}

func (v *stubVerifier) Verify(_ context.Context, token string) (DiscordUser, error) {
	v.token = token
	return v.user, v.error
}

type stubMembers struct {
	found                member.Member
	error                error
	userID               string
	updatedMemberID      int64
	updatedCharacterName string
}

func (m *stubMembers) FindByUserID(_ context.Context, userID string) (member.Member, error) {
	m.userID = userID
	return m.found, m.error
}

func (m *stubMembers) UpdateCharacterName(_ context.Context, memberID int64, characterName string) (member.Member, error) {
	m.updatedMemberID, m.updatedCharacterName = memberID, characterName
	return member.Member{ID: memberID, CharacterName: characterName}, m.error
}

type stubIssuer struct {
	token     string
	expiresAt time.Time
	error     error
	found     member.Member
}

type stubTokens struct {
	claims appauth.Claims
	err    error
}

func (s stubTokens) Verify(string) (appauth.Claims, error) { return s.claims, s.err }

type stubDashboard struct {
	snapshot dashboard.Snapshot
	records  dashboard.MemberRecords
	err      error
	memberID int64
}

type stubAttendance struct {
	report attendancehistory.MonthlyReport
	err    error
	year   int
	month  time.Month
}

type stubSettings struct {
	values  dbsettings.Values
	updated dbsettings.Values
	err     error
}

func (s *stubSettings) Load(context.Context) (dbsettings.Values, error) { return s.values, s.err }
func (s *stubSettings) Update(_ context.Context, values dbsettings.Values) (dbsettings.Values, error) {
	s.updated = values
	if s.err != nil {
		return dbsettings.Values{}, s.err
	}
	return values, nil
}

func (s *stubAttendance) GetMonthly(_ context.Context, year int, month time.Month) (attendancehistory.MonthlyReport, error) {
	s.year = year
	s.month = month
	return s.report, s.err
}

func (s *stubDashboard) Get(_ context.Context, memberID int64) (dashboard.Snapshot, error) {
	s.memberID = memberID
	return s.snapshot, s.err
}

func (s *stubDashboard) GetMemberRecords(_ context.Context, memberID int64) (dashboard.MemberRecords, error) {
	s.memberID = memberID
	return s.records, s.err
}

func (i *stubIssuer) Issue(found member.Member) (string, time.Time, error) {
	i.found = found
	return i.token, i.expiresAt, i.error
}

func TestDiscordLoginIssuesAppTokenForRegisteredMember(t *testing.T) {
	verifier := &stubVerifier{user: DiscordUser{ID: "123", Username: "delta"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", Username: "delta", DisplayName: "Delta"}}
	expiresAt := time.Date(2026, 8, 14, 10, 15, 0, 0, time.UTC)
	issuer := &stubIssuer{token: "app-token", expiresAt: expiresAt}
	recorder := request(NewHandler(verifier, members, issuer, stubTokens{}, &stubDashboard{}, &stubAttendance{}, testLogger()), http.MethodPost, "/api/v1/auth/discord", "Bearer discord-token")

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"access_token":"app-token"`) {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if verifier.token != "discord-token" || members.userID != "123" || issuer.found.ID != 7 {
		t.Fatalf("flow = verifier token %q, member user %q, issuer member %#v", verifier.token, members.userID, issuer.found)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestDiscordLoginRejectsInvalidAndUnregisteredUsers(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		verifyErr  error
		memberErr  error
		wantStatus int
		wantCode   string
	}{
		{name: "missing bearer", wantStatus: http.StatusUnauthorized, wantCode: "INVALID_DISCORD_TOKEN"},
		{name: "invalid Discord token", header: "Bearer bad", verifyErr: ErrInvalidDiscordToken, wantStatus: http.StatusUnauthorized, wantCode: "INVALID_DISCORD_TOKEN"},
		{name: "unregistered member", header: "Bearer valid", memberErr: member.ErrNotFound, wantStatus: http.StatusForbidden, wantCode: "MEMBER_NOT_REGISTERED"},
		{name: "Discord unavailable", header: "Bearer valid", verifyErr: ErrDiscordUnavailable, wantStatus: http.StatusBadGateway, wantCode: "DISCORD_UNAVAILABLE"},
		{name: "database unavailable", header: "Bearer valid", memberErr: errors.New("database unavailable"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_ERROR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &stubVerifier{user: DiscordUser{ID: "123", Username: "delta"}, error: test.verifyErr}
			members := &stubMembers{error: test.memberErr}
			recorder := request(NewHandler(verifier, members, &stubIssuer{}, stubTokens{}, &stubDashboard{}, &stubAttendance{}, testLogger()), http.MethodPost, "/api/v1/auth/discord", test.header)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), test.wantCode) {
				t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestHealthAndMethodRouting(t *testing.T) {
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{}, &stubDashboard{}, &stubAttendance{}, testLogger())
	health := request(handler, http.MethodGet, "/healthz", "")
	if health.Code != http.StatusOK || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health response = %d %s", health.Code, health.Body.String())
	}
	wrongMethod := request(handler, http.MethodGet, "/api/v1/auth/discord", "")
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method response = %d", wrongMethod.Code)
	}
}

func TestDashboardRequiresValidMemberToken(t *testing.T) {
	dashboards := &stubDashboard{snapshot: dashboard.Snapshot{TotalMembers: 12, PlayerThreshold: 15}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, dashboards, &stubAttendance{}, testLogger())

	unauthorized := request(handler, http.MethodGet, "/api/v1/dashboard", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	authorized := request(handler, http.MethodGet, "/api/v1/dashboard", "Bearer app-token")
	if authorized.Code != http.StatusOK || dashboards.memberID != 7 || !strings.Contains(authorized.Body.String(), `"total_members":12`) {
		t.Fatalf("authorized response = %d %s, member = %d", authorized.Code, authorized.Body.String(), dashboards.memberID)
	}
}

func TestMemberRecordsUseLoggedInMember(t *testing.T) {
	dashboards := &stubDashboard{records: dashboard.MemberRecords{TotalPlaytimeSeconds: 3600, TotalAttended: 2}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, dashboards, &stubAttendance{}, testLogger())

	unauthorized := request(handler, http.MethodGet, "/api/v1/me/records", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	authorized := request(handler, http.MethodGet, "/api/v1/me/records", "Bearer app-token")
	if authorized.Code != http.StatusOK || dashboards.memberID != 7 || !strings.Contains(authorized.Body.String(), `"total_playtime_seconds":3600`) {
		t.Fatalf("member records response = %d %s, member = %d", authorized.Code, authorized.Body.String(), dashboards.memberID)
	}
}

func TestMonthlyAttendanceRequiresAuthAndValidMonth(t *testing.T) {
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{Month: "2026-08", TotalAttended: 12}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger())

	unauthorized := request(handler, http.MethodGet, "/api/v1/attendance?month=2026-08", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	invalid := request(handler, http.MethodGet, "/api/v1/attendance?month=August", "Bearer app-token")
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "INVALID_MONTH") {
		t.Fatalf("invalid month response = %d %s", invalid.Code, invalid.Body.String())
	}
	authorized := request(handler, http.MethodGet, "/api/v1/attendance?month=2026-08", "Bearer app-token")
	if authorized.Code != http.StatusOK || reader.year != 2026 || reader.month != time.August || !strings.Contains(authorized.Body.String(), `"total_attended":12`) {
		t.Fatalf("authorized response = %d %s, month = %d-%d", authorized.Code, authorized.Body.String(), reader.year, reader.month)
	}
}

func TestMyMonthlyAttendanceOnlyReturnsLoggedInMember(t *testing.T) {
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{
		Month: "2026-08", AttendanceDays: []string{"2026-08-14"}, TotalAttended: 2, TotalOpportunities: 2,
		Members: []attendancehistory.MemberRecord{
			{MemberID: 3, DisplayName: "Other", TotalAttended: 1},
			{MemberID: 7, DisplayName: "Logged In", TotalAttended: 1},
		},
	}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger())

	response := request(handler, http.MethodGet, "/api/v1/attendance/me?month=2026-08", "Bearer app-token")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "Other") || !strings.Contains(response.Body.String(), "Logged In") {
		t.Fatalf("personal attendance response = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"total_attended":1`) || !strings.Contains(response.Body.String(), `"total_opportunities":1`) {
		t.Fatalf("personal attendance totals = %s", response.Body.String())
	}
}

func TestMonthlyPayslipsRequiresAuthAndCalculatesEveryMember(t *testing.T) {
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{Month: "2026-08", Members: []attendancehistory.MemberRecord{
		{MemberID: 1, CharacterName: "Below", TotalAttended: 23},
		{MemberID: 2, CharacterName: "Qualified", TotalAttended: 24},
	}}}
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30"}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger(), store)

	unauthorized := request(handler, http.MethodGet, "/api/v1/payslips?month=2026-08", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	response := request(handler, http.MethodGet, "/api/v1/payslips?month=2026-08", "Bearer app-token")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"eligible_players":1`) || !strings.Contains(response.Body.String(), `"payout":"8000000"`) || !strings.Contains(response.Body.String(), `"payout":"0"`) {
		t.Fatalf("payslip response = %d %s", response.Code, response.Body.String())
	}
}

func TestSettingsRequireAuthAndValidateUpdates(t *testing.T) {
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), store)

	unauthorized := request(handler, http.MethodGet, "/api/v1/settings", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d", unauthorized.Code)
	}
	loaded := request(handler, http.MethodGet, "/api/v1/settings", "Bearer app-token")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"start_attendance":"21:00"`) || !strings.Contains(loaded.Body.String(), `"is_admin":true`) {
		t.Fatalf("settings response = %d %s", loaded.Code, loaded.Body.String())
	}

	invalidRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{"start_attendance":"21:00","end_attendance":"21:00","playtime_threshold":"90m","player_threshold":"15","payment_contract":"8000000","attendance_minimum":"24","attendance_maximum":"30"}`))
	invalidRequest.Header.Set("Authorization", "Bearer app-token")
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "must differ") {
		t.Fatalf("invalid response = %d %s", invalid.Code, invalid.Body.String())
	}

	validRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{"start_attendance":"20:30","end_attendance":"01:30","playtime_threshold":"1h30m","player_threshold":"20","payment_contract":"9000000","attendance_minimum":"24","attendance_maximum":"30"}`))
	validRequest.Header.Set("Authorization", "Bearer app-token")
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, validRequest)
	if valid.Code != http.StatusOK || store.updated.PlayerThreshold != "20" || store.updated.PaymentContract != "9000000" {
		t.Fatalf("valid response = %d %s, updated = %#v", valid.Code, valid.Body.String(), store.updated)
	}
}

func TestSettingsUpdateRejectsNonAdmin(t *testing.T) {
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: false}}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), store)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer app-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "ADMIN_REQUIRED") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestUpdateMyProfileUsesAuthenticatedMember(t *testing.T) {
	members := &stubMembers{}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, &stubAttendance{}, testLogger())
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", strings.NewReader(`{"character_name":"  Kenji Nakamura  "}`))
	request.Header.Set("Authorization", "Bearer app-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || members.updatedMemberID != 7 || members.updatedCharacterName != "Kenji Nakamura" {
		t.Fatalf("response = %d %s, update = %d %q", response.Code, response.Body.String(), members.updatedMemberID, members.updatedCharacterName)
	}
}

func request(handler http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", authorization)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
