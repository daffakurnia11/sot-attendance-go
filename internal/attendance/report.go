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

type MemberRecord struct {
	MemberID      int64         `json:"member_id"`
	Username      string        `json:"username"`
	DisplayName   string        `json:"display_name"`
	CharacterName string        `json:"character_name"`
	TotalAttended int           `json:"total_attended"`
	Records       []DailyRecord `json:"records"`
}

type MonthlyReport struct {
	Month              string         `json:"month"`
	DaysInMonth        int            `json:"days_in_month"`
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

func (r *ReportRepository) GetMonthly(ctx context.Context, year int, month time.Month) (MonthlyReport, error) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, r.location)
	end := start.AddDate(0, 1, 0)
	report := MonthlyReport{
		Month:          start.Format("2006-01"),
		DaysInMonth:    end.AddDate(0, 0, -1).Day(),
		AttendanceDays: make([]string, 0),
		Members:        make([]MemberRecord, 0),
	}

	const membersQuery = `
		SELECT id, username, display_name, COALESCE(character_name, '')
		FROM members
		ORDER BY display_name, id`
	memberRows, err := r.database.Query(ctx, membersQuery)
	if err != nil {
		return MonthlyReport{}, fmt.Errorf("query attendance members: %w", err)
	}
	memberIndexes := make(map[int64]int)
	for memberRows.Next() {
		var record MemberRecord
		if err := memberRows.Scan(&record.MemberID, &record.Username, &record.DisplayName, &record.CharacterName); err != nil {
			memberRows.Close()
			return MonthlyReport{}, fmt.Errorf("scan attendance member: %w", err)
		}
		record.Records = make([]DailyRecord, 0)
		memberIndexes[record.MemberID] = len(report.Members)
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
				member_id,
				(attendance_start AT TIME ZONE $3)::date AS attendance_date,
				is_attended,
				playtime
			FROM attendance_logs
			WHERE attendance_start >= $1 AND attendance_start < $2
		)
		SELECT
			member_id,
			TO_CHAR(attendance_date, 'YYYY-MM-DD'),
			BOOL_OR(is_attended),
			SUM(EXTRACT(EPOCH FROM playtime))::bigint
		FROM daily_attendance
		GROUP BY member_id, attendance_date
		ORDER BY attendance_date, member_id`
	attendanceRows, err := r.database.Query(ctx, attendanceQuery, start, end, r.location.String())
	if err != nil {
		return MonthlyReport{}, fmt.Errorf("query monthly attendance: %w", err)
	}
	defer attendanceRows.Close()
	attendanceDays := make(map[string]struct{})
	for attendanceRows.Next() {
		var memberID int64
		var daily DailyRecord
		if err := attendanceRows.Scan(&memberID, &daily.Date, &daily.IsAttended, &daily.PlaytimeSeconds); err != nil {
			return MonthlyReport{}, fmt.Errorf("scan monthly attendance: %w", err)
		}
		attendanceDays[daily.Date] = struct{}{}
		memberIndex, found := memberIndexes[memberID]
		if !found {
			continue
		}
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
