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
		PaymentContract:   validated.PaymentContract,
		AttendanceMinimum: minimum,
		AttendanceMaximum: maximum,
		TotalPlayers:      len(attendanceReport.Members),
		Players:           make([]Player, 0, len(attendanceReport.Members)),
	}
	for _, member := range attendanceReport.Members {
		if member.TotalAttended >= minimum {
			report.EligiblePlayers++
		}
	}
	var eligiblePayout int64
	if report.EligiblePlayers > 0 {
		eligiblePayout = (payment / int64(report.EligiblePlayers) / 1000) * 1000
	}
	var total int64
	for _, member := range attendanceReport.Members {
		player := Player{
			MemberID: member.MemberID, Username: member.Username, DisplayName: member.DisplayName,
			CharacterName: member.CharacterName, AttendedDays: member.TotalAttended,
		}
		if member.TotalAttended >= minimum {
			player.Eligible = true
			player.Payout = strconv.FormatInt(eligiblePayout, 10)
			total += eligiblePayout
		} else {
			player.Payout = "0"
		}
		report.Players = append(report.Players, player)
	}
	report.TotalPayout = strconv.FormatInt(total, 10)
	return report, nil
}
