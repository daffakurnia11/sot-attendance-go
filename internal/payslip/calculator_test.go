package payslip

import (
	"testing"

	"github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	"github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

func TestCalculateSharesContractByAttendedDays(t *testing.T) {
	// Counted days are 24, 30 (capped from 31) and 25, so the pool divides by
	// 79 and each player draws their own share of it.
	report, err := Calculate(attendance.MonthlyReport{Month: "2026-08", Members: []attendance.MemberRecord{
		{MemberID: 1, CharacterName: "Below", TotalAttended: 23},
		{MemberID: 2, CharacterName: "Qualified", TotalAttended: 24},
		{MemberID: 3, CharacterName: "Capped", TotalAttended: 31},
		{MemberID: 4, CharacterName: "Rounded", TotalAttended: 25},
	}}, settings.Values{
		StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15",
		PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.EligiblePlayers != 3 || report.Players[0].Eligible || report.Players[0].Payout != "0" {
		t.Fatalf("eligibility = %#v", report)
	}
	if report.Players[1].Payout != "2430000" || report.Players[3].Payout != "2531000" {
		t.Fatalf("payouts = %#v", report)
	}
	// 31 days counts as 30: the capped player must not out-earn the 30-day
	// share their attendance is worth.
	if report.Players[2].Payout != "3037000" {
		t.Fatalf("capped payout = %q, want 3037000", report.Players[2].Payout)
	}
	if report.TotalPayout != "7998000" {
		t.Fatalf("total payout = %q, want 7998000", report.TotalPayout)
	}
}

func TestCalculatePaysMoreForMoreAttendance(t *testing.T) {
	// The bug this replaced: every eligible player drew an identical payout no
	// matter how many days they attended.
	report, err := Calculate(attendance.MonthlyReport{Month: "2026-08", Members: []attendance.MemberRecord{
		{MemberID: 1, CharacterName: "Once", TotalAttended: 1},
		{MemberID: 2, CharacterName: "Twice", TotalAttended: 2},
	}}, settings.Values{
		StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15",
		PaymentContract: "8000000", AttendanceMinimum: "1", AttendanceMaximum: "30", StartDateContract: "28",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Players[0].Payout != "2666000" || report.Players[1].Payout != "5333000" {
		t.Fatalf("payouts = %#v", report.Players)
	}
}

func TestCalculateHandlesNoEligiblePlayers(t *testing.T) {
	report, err := Calculate(attendance.MonthlyReport{Month: "2026-08", Members: []attendance.MemberRecord{{MemberID: 1, TotalAttended: 0}}}, settings.Values{
		StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15",
		PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30", StartDateContract: "28",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.EligiblePlayers != 0 || report.Players[0].Payout != "0" || report.TotalPayout != "0" {
		t.Fatalf("report = %#v", report)
	}
}

func TestCalculateRejectsInvalidSettings(t *testing.T) {
	_, err := Calculate(attendance.MonthlyReport{}, settings.Values{})
	if err == nil {
		t.Fatal("expected invalid settings error")
	}
}
