package attendance

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DailyRecord struct {
	Date            string `json:"date"`
	IsAttended      bool   `json:"is_attended"`
	PlaytimeSeconds int64  `json:"playtime_seconds"`
}

// MemberID identifies the representative server_members row, not an auth account.
type MemberRecord struct {
	MemberID      int64         `json:"member_id"`
	DiscordUserID string        `json:"discord_user_id"`
	Username      string        `json:"username"`
	DisplayName   string        `json:"display_name"`
	CharacterName string        `json:"character_name"`
	TotalAttended int           `json:"total_attended"`
	Records       []DailyRecord `json:"records"`
}

type MonthlyReport struct {
	Month              string         `json:"month"`
	DaysInMonth        int            `json:"days_in_month"`
	PeriodStart        string         `json:"period_start"`
	PeriodEnd          string         `json:"period_end"`
	PeriodDates        []string       `json:"period_dates"`
	AttendanceDays     []string       `json:"attendance_days"`
	TotalAttended      int            `json:"total_attended"`
	TotalOpportunities int            `json:"total_opportunities"`
	Members            []MemberRecord `json:"members"`
}

type ReportRepository struct {
	database *pgxpool.Pool
	location *time.Location
}

func NewReportRepository(database *pgxpool.Pool, location *time.Location) *ReportRepository {
	return &ReportRepository{database: database, location: location}
}

func (r *ReportRepository) GetMonthly(ctx context.Context, year int, month time.Month, contractStartDay int) (MonthlyReport, error) {
	start := contractBoundary(year, month, contractStartDay, r.location)
	nextMonth := time.Date(year, month, 1, 0, 0, 0, 0, r.location).AddDate(0, 1, 0)
	end := contractBoundary(nextMonth.Year(), nextMonth.Month(), contractStartDay, r.location)
	periodDates := make([]string, 0, int(end.Sub(start).Hours()/24))
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		periodDates = append(periodDates, date.Format("2006-01-02"))
	}
	report := MonthlyReport{
		Month:          start.Format("2006-01"),
		DaysInMonth:    len(periodDates),
		PeriodStart:    start.Format("2006-01-02"),
		PeriodEnd:      end.AddDate(0, 0, -1).Format("2006-01-02"),
		PeriodDates:    periodDates,
		AttendanceDays: make([]string, 0),
		Members:        make([]MemberRecord, 0),
	}

	// The server roster owns attendance identity. The latest character supplies
	// the name; attendance remains one row per Discord account.
	const membersQuery = `
		SELECT DISTINCT ON (sm.discord_user_id)
			sm.id, sm.discord_user_id, sm.username, sm.player_name, sm.player_name
		FROM server_members sm
		WHERE COALESCE(sm.discord_user_id, '') <> ''
		ORDER BY sm.discord_user_id, sm.updated_at DESC, sm.id DESC`
	memberRows, err := r.database.Query(ctx, membersQuery)
	if err != nil {
		return MonthlyReport{}, fmt.Errorf("query attendance members: %w", err)
	}
	memberIndexes := make(map[string]int)
	for memberRows.Next() {
		var record MemberRecord
		if err := memberRows.Scan(&record.MemberID, &record.DiscordUserID, &record.Username, &record.DisplayName, &record.CharacterName); err != nil {
			memberRows.Close()
			return MonthlyReport{}, fmt.Errorf("scan attendance member: %w", err)
		}
		record.Records = make([]DailyRecord, 0)
		memberIndexes[record.DiscordUserID] = len(report.Members)
		report.Members = append(report.Members, record)
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		return MonthlyReport{}, fmt.Errorf("iterate attendance members: %w", err)
	}
	memberRows.Close()

	const attendanceQuery = `
		WITH daily_attendance AS (
			SELECT
				COALESCE(sm.discord_user_id, m.discord_user_id) AS discord_user_id,
				(attendance_start AT TIME ZONE $3)::date AS attendance_date,
				is_attended,
				playtime
			FROM attendance_logs a
			LEFT JOIN server_members sm ON sm.id = a.server_member_id
			LEFT JOIN members m ON m.id = a.member_id AND a.server_member_id IS NULL
			WHERE attendance_start >= $1 AND attendance_start < $2
		)
		SELECT
			discord_user_id,
			TO_CHAR(attendance_date, 'YYYY-MM-DD'),
			BOOL_OR(is_attended),
			SUM(EXTRACT(EPOCH FROM playtime))::bigint
		FROM daily_attendance
		GROUP BY discord_user_id, attendance_date
		HAVING discord_user_id IS NOT NULL
		ORDER BY attendance_date, discord_user_id`
	attendanceRows, err := r.database.Query(ctx, attendanceQuery, start, end, r.location.String())
	if err != nil {
		return MonthlyReport{}, fmt.Errorf("query monthly attendance: %w", err)
	}
	defer attendanceRows.Close()
	attendanceDays := make(map[string]struct{})
	for attendanceRows.Next() {
		var discordUserID string
		var daily DailyRecord
		if err := attendanceRows.Scan(&discordUserID, &daily.Date, &daily.IsAttended, &daily.PlaytimeSeconds); err != nil {
			return MonthlyReport{}, fmt.Errorf("scan monthly attendance: %w", err)
		}
		memberIndex, found := memberIndexes[discordUserID]
		if !found {
			continue
		}
		attendanceDays[daily.Date] = struct{}{}
		report.Members[memberIndex].Records = append(report.Members[memberIndex].Records, daily)
		if daily.IsAttended {
			report.Members[memberIndex].TotalAttended++
			report.TotalAttended++
		}
	}
	if err := attendanceRows.Err(); err != nil {
		return MonthlyReport{}, fmt.Errorf("iterate monthly attendance: %w", err)
	}
	for date := range attendanceDays {
		report.AttendanceDays = append(report.AttendanceDays, date)
	}
	slices.Sort(report.AttendanceDays)
	report.TotalOpportunities = len(report.Members) * len(report.AttendanceDays)
	return report, nil
}

func contractBoundary(year int, month time.Month, day int, location *time.Location) time.Time {
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, location).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}
