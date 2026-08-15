package payslip

import (
	"fmt"
	"strconv"

	"github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	"github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

type Player struct {
	MemberID      int64  `json:"member_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	CharacterName string `json:"character_name"`
	AttendedDays  int    `json:"attended_days"`
	Eligible      bool   `json:"eligible"`
	Payout        string `json:"payout"`
}

type Report struct {
	Month             string   `json:"month"`
	PeriodStart       string   `json:"period_start"`
	PeriodEnd         string   `json:"period_end"`
	PaymentContract   string   `json:"payment_contract"`
	AttendanceMinimum int      `json:"attendance_minimum"`
	AttendanceMaximum int      `json:"attendance_maximum"`
	TotalPlayers      int      `json:"total_players"`
	EligiblePlayers   int      `json:"eligible_players"`
	TotalPayout       string   `json:"total_payout"`
	Players           []Player `json:"players"`
}

func Calculate(attendanceReport attendance.MonthlyReport, values settings.Values) (Report, error) {
	validated, err := settings.Validate(values)
	if err != nil {
		return Report{}, fmt.Errorf("validate payslip settings: %w", err)
	}
	payment, _ := strconv.ParseInt(validated.PaymentContract, 10, 64)
	minimum, _ := strconv.Atoi(validated.AttendanceMinimum)
	maximum, _ := strconv.Atoi(validated.AttendanceMaximum)

	report := Report{
		Month:             attendanceReport.Month,
		PeriodStart:       attendanceReport.PeriodStart,
		PeriodEnd:         attendanceReport.PeriodEnd,
		PaymentContract:   validated.PaymentContract,
		AttendanceMinimum: minimum,
		AttendanceMaximum: maximum,
		TotalPlayers:      len(attendanceReport.Members),
		Players:           make([]Player, 0, len(attendanceReport.Members)),
	}
	// The contract is shared out by attended days rather than split evenly, so
	// attending twice as often earns twice as much. Dividing by the days
	// actually recorded, instead of by attendance_maximum, keeps the whole
	// contract distributed however many sessions a period happens to hold: an
	// even split over a fixed 30-day divisor would pay out almost nothing early
	// in a period and leave the remainder to be reconciled by hand.
	//
	// attendance_minimum still gates eligibility outright, and days beyond
	// attendance_maximum stop accruing, so no one can dilute the pool by
	// attending more often than the contract allows for.
	var countedDays int64
	for _, member := range attendanceReport.Members {
		if member.TotalAttended >= minimum {
			report.EligiblePlayers++
			countedDays += int64(min(member.TotalAttended, maximum))
		}
	}
	var total int64
	for _, member := range attendanceReport.Members {
		player := Player{
			MemberID: member.MemberID, Username: member.Username, DisplayName: member.DisplayName,
			CharacterName: member.CharacterName, AttendedDays: member.TotalAttended,
		}
		player.Payout = "0"
		if member.TotalAttended >= minimum && countedDays > 0 {
			player.Eligible = true
			// Rounded down to the nearest 1000 rupiah per player, as before.
			payout := (payment * int64(min(member.TotalAttended, maximum)) / countedDays / 1000) * 1000
			player.Payout = strconv.FormatInt(payout, 10)
			total += payout
		}
		report.Players = append(report.Players, player)
	}
	report.TotalPayout = strconv.FormatInt(total, 10)
	return report, nil
}
