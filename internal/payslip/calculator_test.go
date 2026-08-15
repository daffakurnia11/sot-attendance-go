package payslip

import (
	"testing"

	"github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	"github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

func TestCalculateDividesContractAcrossEligiblePlayers(t *testing.T) {
	report, err := Calculate(attendance.MonthlyReport{Month: "2026-08", Members: []attendance.MemberRecord{
		{MemberID: 1, CharacterName: "Below", TotalAttended: 23},
		{MemberID: 2, CharacterName: "Qualified", TotalAttended: 24},
		{MemberID: 3, CharacterName: "Capped", TotalAttended: 31},
		{MemberID: 4, CharacterName: "Rounded", TotalAttended: 25},
	}}, settings.Values{
		StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15",
		PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.EligiblePlayers != 3 || report.Players[0].Eligible || report.Players[0].Payout != "0" {
		t.Fatalf("eligibility = %#v", report)
	}
	if report.Players[1].Payout != "2666000" || report.Players[2].Payout != "2666000" || report.Players[3].Payout != "2666000" || report.TotalPayout != "7998000" {
		t.Fatalf("payouts = %#v", report)
	}
}

func TestCalculateHandlesNoEligiblePlayers(t *testing.T) {
	report, err := Calculate(attendance.MonthlyReport{Month: "2026-08", Members: []attendance.MemberRecord{{MemberID: 1, TotalAttended: 0}}}, settings.Values{
		StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15",
		PaymentContract: "8000000", AttendanceMinimum: "24", AttendanceMaximum: "30",
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
