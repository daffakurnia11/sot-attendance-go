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
	"github.com/daffakurniawan/sot-discord-bot/internal/crafting"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
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
	updatedCFXName       string
}

func (m *stubMembers) FindByUserID(_ context.Context, userID string) (member.Member, error) {
	m.userID = userID
	return m.found, m.error
}

func (m *stubMembers) UpdateProfile(_ context.Context, memberID int64, characterName, cfxName string) (member.Member, error) {
	m.updatedMemberID, m.updatedCharacterName, m.updatedCFXName = memberID, characterName, cfxName
	return member.Member{ID: memberID, CharacterName: characterName, CFXName: cfxName}, m.error
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
	report   attendancehistory.MonthlyReport
	err      error
	year     int
	month    time.Month
	startDay int
}

type stubSettings struct {
	values  dbsettings.Values
	updated dbsettings.Values
	err     error
}

type stubCrafting struct {
	recipes    []crafting.RecipeSummary
	recipe     crafting.Recipe
	err        error
	weaponCode string
}

type stubMoneyLedger struct {
	entries  []moneydomain.LedgerEntry
	balances map[moneydomain.Account]int64
	account  moneydomain.Account
	err      error
}

func (s *stubMoneyLedger) Balance(_ context.Context, account moneydomain.Account) (int64, error) {
	return s.balances[account], s.err
}

func (s *stubMoneyLedger) List(_ context.Context, account moneydomain.Account) ([]moneydomain.LedgerEntry, error) {
	s.account = account
	return s.entries, s.err
}

func (s *stubCrafting) List(context.Context) ([]crafting.RecipeSummary, error) {
	return s.recipes, s.err
}

func (s *stubCrafting) Get(_ context.Context, weaponCode string) (crafting.Recipe, error) {
	s.weaponCode = weaponCode
	return s.recipe, s.err
}

func (s *stubSettings) Load(context.Context) (dbsettings.Values, error) { return s.values, s.err }
func (s *stubSettings) Update(_ context.Context, values dbsettings.Values) (dbsettings.Values, error) {
	s.updated = values
	if s.err != nil {
		return dbsettings.Values{}, s.err
	}
	return values, nil
}

func (s *stubAttendance) GetMonthly(_ context.Context, year int, month time.Month, startDay int) (attendancehistory.MonthlyReport, error) {
	s.year = year
	s.month = month
	s.startDay = startDay
	return s.report, s.err
}

func TestMonthlyAttendanceUsesConfiguredContractStartDate(t *testing.T) {
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{Month: "2026-08"}}
	store := &stubSettings{values: dbsettings.Values{StartDateContract: "28"}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger(), store)

	response := request(handler, http.MethodGet, "/api/v1/attendance?month=2026-08", "Bearer app-token")
	if response.Code != http.StatusOK || reader.startDay != 28 {
		t.Fatalf("response = %d %s, start day = %d", response.Code, response.Body.String(), reader.startDay)
	}
}

func TestRequestedPeriodUsesActiveContractMonth(t *testing.T) {
	year, month, err := requestedPeriod("", time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC), 28)
	if err != nil || year != 2026 || month != time.July {
		t.Fatalf("requestedPeriod() = %d-%d, %v; want 2026-07", year, month, err)
	}
	year, month, err = requestedPeriod("", time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC), 28)
	if err != nil || year != 2026 || month != time.August {
		t.Fatalf("requestedPeriod() = %d-%d, %v; want 2026-08", year, month, err)
	}
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

func TestCraftingRecipesRequireAuthAndCalculateTotals(t *testing.T) {
	store := &stubCrafting{
		recipes: []crafting.RecipeSummary{{WeaponCode: "desert_eagle", WeaponName: "Desert Eagle", OutputQuantity: 1, CraftingTimeSeconds: 8}},
		recipe: crafting.Recipe{
			RecipeSummary: crafting.RecipeSummary{WeaponCode: "desert_eagle", WeaponName: "Desert Eagle", OutputQuantity: 1, CraftingTimeSeconds: 8},
			Ingredients:   []crafting.Ingredient{{ItemCode: "iron", ItemName: "Iron", Quantity: 25}},
		},
	}
	handler := NewHandlerWithCrafting(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, store)
	unauthorized := request(handler, http.MethodGet, "/api/v1/crafting/recipes", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	listed := request(handler, http.MethodGet, "/api/v1/crafting/recipes", "Bearer app-token")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"weapon_code":"desert_eagle"`) {
		t.Fatalf("list response = %d %s", listed.Code, listed.Body.String())
	}
	calculationRequest := httptest.NewRequest(http.MethodPost, "/api/v1/crafting/calculate", strings.NewReader(`{"weapon_code":"desert_eagle","quantity":3}`))
	calculationRequest.Header.Set("Authorization", "Bearer app-token")
	calculation := httptest.NewRecorder()
	handler.ServeHTTP(calculation, calculationRequest)
	if calculation.Code != http.StatusOK || store.weaponCode != "desert_eagle" || !strings.Contains(calculation.Body.String(), `"total_quantity":75`) || !strings.Contains(calculation.Body.String(), `"crafting_time_seconds":24`) {
		t.Fatalf("calculation response = %d %s, weapon = %q", calculation.Code, calculation.Body.String(), store.weaponCode)
	}
}

func TestCraftingCalculationRejectsInvalidQuantityAndUnknownRecipe(t *testing.T) {
	store := &stubCrafting{err: crafting.ErrNotFound}
	handler := NewHandlerWithCrafting(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, store)
	for body, wantStatus := range map[string]int{
		`{"weapon_code":"desert_eagle","quantity":0}`: http.StatusUnprocessableEntity,
		`{"weapon_code":"missing","quantity":1}`:      http.StatusNotFound,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/crafting/calculate", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer app-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != wantStatus {
			t.Fatalf("body %s response = %d %s, want %d", body, response.Code, response.Body.String(), wantStatus)
		}
	}
}

func TestBatchCraftingCalculationTotalsRecipes(t *testing.T) {
	store := &stubCrafting{recipe: crafting.Recipe{
		RecipeSummary: crafting.RecipeSummary{WeaponCode: "weapon", WeaponName: "Weapon", OutputQuantity: 1, CraftingTimeSeconds: 8},
		Ingredients:   []crafting.Ingredient{{ItemCode: "iron", ItemName: "Iron", Quantity: 20}},
	}}
	handler := NewHandlerWithCrafting(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, store)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/crafting/calculate-batch", strings.NewReader(`{"recipes":[{"weapon_code":"one","quantity":2},{"weapon_code":"two","quantity":3}]}`))
	request.Header.Set("Authorization", "Bearer app-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total_requested_quantity":5`) || !strings.Contains(response.Body.String(), `"total_quantity":100`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestDashboardRequiresValidMemberToken(t *testing.T) {
	dashboards := &stubDashboard{snapshot: dashboard.Snapshot{
		TotalMembers:    12,
		PlayerThreshold: 15,
		DiscordPlayers:  []dashboard.Player{{MemberID: 7, CFXName: "SOT - Kenji"}},
	}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, dashboards, &stubAttendance{}, testLogger())

	unauthorized := request(handler, http.MethodGet, "/api/v1/dashboard", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized response = %d %s", unauthorized.Code, unauthorized.Body.String())
	}
	authorized := request(handler, http.MethodGet, "/api/v1/dashboard", "Bearer app-token")
	if authorized.Code != http.StatusOK || dashboards.memberID != 7 || !strings.Contains(authorized.Body.String(), `"total_members":12`) || !strings.Contains(authorized.Body.String(), `"cfx_name":"SOT - Kenji"`) {
		t.Fatalf("authorized response = %d %s, member = %d", authorized.Code, authorized.Body.String(), dashboards.memberID)
	}
}

func TestMoneyTransactionsRequireAdminAndUseRequestedAccount(t *testing.T) {
	ledger := &stubMoneyLedger{balances: map[moneydomain.Account]int64{moneydomain.AccountOffice: 1012500, moneydomain.AccountDirty: 18000}, entries: []moneydomain.LedgerEntry{{ID: 9, Account: moneydomain.AccountDirty, Type: "deposit", Amount: 18000, ActorName: "Kenji"}}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}
	handler := NewHandlerWithCrafting(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger(), nil, nil, ledger)

	response := request(handler, http.MethodGet, "/api/v1/money-transactions/dirty", "Bearer app-token")
	if response.Code != http.StatusOK || ledger.account != moneydomain.AccountDirty || !strings.Contains(response.Body.String(), `"actor_name":"Kenji"`) || !strings.Contains(response.Body.String(), `"current_balance":18000`) || !strings.Contains(response.Body.String(), `"office":1012500`) {
		t.Fatalf("response = %d %s, account = %q", response.Code, response.Body.String(), ledger.account)
	}

	members.found.IsAdmin = false
	response = request(handler, http.MethodGet, "/api/v1/money-transactions/office", "Bearer app-token")
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin response = %d %s", response.Code, response.Body.String())
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
	handler := NewHandler(&stubVerifier{}, &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger())

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
	handler := NewHandler(&stubVerifier{}, &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger())

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
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28"}}
	handler := NewHandler(&stubVerifier{}, &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: true}}, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7}}, &stubDashboard{}, reader, testLogger(), store)

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
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28"}}
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

	invalidRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{"start_attendance":"21:00","end_attendance":"21:00","playtime_threshold":"90m","player_threshold":"15","payment_contract":"8000000","attendance_minimum":"24","attendance_maximum":"30","start_date_contract":"28","office_money_balance":"1012500","dirty_money_balance":"18000"}`))
	invalidRequest.Header.Set("Authorization", "Bearer app-token")
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, invalidRequest)
	if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), "must differ") {
		t.Fatalf("invalid response = %d %s", invalid.Code, invalid.Body.String())
	}

	validRequest := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{"start_attendance":"20:30","end_attendance":"01:30","playtime_threshold":"1h30m","player_threshold":"20","payment_contract":"9000000","attendance_minimum":"24","attendance_maximum":"30","start_date_contract":"28","office_money_balance":"1012500","dirty_money_balance":"18000"}`))
	validRequest.Header.Set("Authorization", "Bearer app-token")
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, validRequest)
	if valid.Code != http.StatusOK || store.updated.PlayerThreshold != "20" || store.updated.PaymentContract != "9000000" || store.updated.OfficeMoneyBalance != "1012500" || store.updated.DirtyMoneyBalance != "18000" {
		t.Fatalf("valid response = %d %s, updated = %#v", valid.Code, valid.Body.String(), store.updated)
	}
}

func TestSettingsUpdateRejectsNonAdmin(t *testing.T) {
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28"}}
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
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/me/profile", strings.NewReader(`{"character_name":"  Kenji Nakamura  ","cfx_name":"  SOT - Kenji  "}`))
	request.Header.Set("Authorization", "Bearer app-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || members.updatedMemberID != 7 || members.updatedCharacterName != "Kenji Nakamura" || members.updatedCFXName != "SOT - Kenji" {
		t.Fatalf("response = %d %s, update = %d %q %q", response.Code, response.Body.String(), members.updatedMemberID, members.updatedCharacterName, members.updatedCFXName)
	}
}

func TestGetMyProfileLoadsCurrentDatabaseValues(t *testing.T) {
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", CharacterName: "Kenji Nakamura", CFXName: "SOT - Kenji"}}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, &stubAttendance{}, testLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/profile", nil)
	request.Header.Set("Authorization", "Bearer app-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cfx_name":"SOT - Kenji"`) || members.userID != "123" {
		t.Fatalf("response = %d %s, user = %q", response.Code, response.Body.String(), members.userID)
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

func TestRosterWideReportsRejectNonAdmin(t *testing.T) {
	// The attendance grid covers every member and the payslip report covers
	// everyone's payout, so both are administrator-only.
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: false}}
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{Month: "2026-08"}}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, reader, testLogger(), store)

	for _, path := range []string{"/api/v1/attendance", "/api/v1/payslips"} {
		response := request(handler, http.MethodGet, path, "Bearer app-token")
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "ADMIN_REQUIRED") {
			t.Fatalf("%s response = %d %s, want 403 ADMIN_REQUIRED", path, response.Code, response.Body.String())
		}
	}
}

func TestMemberOwnReportsStayOpenToNonAdmin(t *testing.T) {
	// A member's own figures are theirs to read; only roster-wide views are
	// gated, so the personal endpoints must not regress with the admin change.
	store := &stubSettings{values: dbsettings.Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28"}}
	members := &stubMembers{found: member.Member{ID: 7, UserID: "123", IsAdmin: false}}
	reader := &stubAttendance{report: attendancehistory.MonthlyReport{Month: "2026-08", AttendanceDays: []string{"2026-08-05"}}}
	handler := NewHandler(&stubVerifier{}, members, &stubIssuer{}, stubTokens{claims: appauth.Claims{MemberID: 7, DiscordUserID: "123"}}, &stubDashboard{}, reader, testLogger(), store)

	for _, path := range []string{"/api/v1/attendance/me", "/api/v1/me/records", "/api/v1/dashboard"} {
		response := request(handler, http.MethodGet, path, "Bearer app-token")
		if response.Code != http.StatusOK {
			t.Fatalf("%s response = %d %s, want 200", path, response.Code, response.Body.String())
		}
	}
}
