package settings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Attendance struct {
	StartTime         time.Duration
	EndTime           time.Duration
	PlaytimeThreshold time.Duration
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{ database queryRower }

func NewRepository(database queryRower) *Repository { return &Repository{database: database} }

func (r *Repository) LoadAttendance(ctx context.Context) (Attendance, error) {
	const query = `
		SELECT
			COALESCE(MAX(value) FILTER (WHERE settings = 'start_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'end_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'playtime_threshold'), '')
		FROM settings`

	var startValue, endValue, playtimeValue string
	if err := r.database.QueryRow(ctx, query).Scan(&startValue, &endValue, &playtimeValue); err != nil {
		return Attendance{}, fmt.Errorf("load attendance settings: %w", err)
	}

	startTime, err := parseClockTime(startValue)
	if err != nil {
		return Attendance{}, fmt.Errorf("setting start_attendance: %w", err)
	}
	endTime, err := parseClockTime(endValue)
	if err != nil {
		return Attendance{}, fmt.Errorf("setting end_attendance: %w", err)
	}
	if startTime == endTime {
		return Attendance{}, errors.New("settings start_attendance and end_attendance must differ")
	}
	playtimeThreshold, err := time.ParseDuration(strings.TrimSpace(playtimeValue))
	if err != nil || playtimeThreshold <= 0 {
		return Attendance{}, errors.New("setting playtime_threshold must be a positive Go duration such as 90m or 1h30m")
	}

	return Attendance{StartTime: startTime, EndTime: endTime, PlaytimeThreshold: playtimeThreshold}, nil
}

func parseClockTime(value string) (time.Duration, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, errors.New("must use HH:MM 24-hour format")
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}
