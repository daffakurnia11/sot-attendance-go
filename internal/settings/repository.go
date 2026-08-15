package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Attendance struct {
	StartTime         time.Duration
	EndTime           time.Duration
	PlaytimeThreshold time.Duration
}

type Values struct {
	StartAttendance   string `json:"start_attendance"`
	EndAttendance     string `json:"end_attendance"`
	PlaytimeThreshold string `json:"playtime_threshold"`
	PlayerThreshold   string `json:"player_threshold"`
	PaymentContract   string `json:"payment_contract"`
	AttendanceMinimum string `json:"attendance_minimum"`
	AttendanceMaximum string `json:"attendance_maximum"`
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{ database queryRower }

func NewRepository(database queryRower) *Repository { return &Repository{database: database} }

func (r *Repository) Load(ctx context.Context) (Values, error) {
	const query = `
		SELECT
			COALESCE(MAX(value) FILTER (WHERE settings = 'start_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'end_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'playtime_threshold'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'player_threshold'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'payment_contract'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'attendance_minimum'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'attendance_maximum'), '')
		FROM settings`
	var values Values
	if err := r.database.QueryRow(ctx, query).Scan(&values.StartAttendance, &values.EndAttendance, &values.PlaytimeThreshold, &values.PlayerThreshold, &values.PaymentContract, &values.AttendanceMinimum, &values.AttendanceMaximum); err != nil {
		return Values{}, fmt.Errorf("load settings: %w", err)
	}
	return values, nil
}

func (r *Repository) Update(ctx context.Context, values Values) (Values, error) {
	normalized, err := Validate(values)
	if err != nil {
		return Values{}, err
	}
	const query = `
		WITH input(settings, value) AS (VALUES
			('start_attendance', $1::text), ('end_attendance', $2::text),
			('playtime_threshold', $3::text), ('player_threshold', $4::text),
			('payment_contract', $5::text), ('attendance_minimum', $6::text),
			('attendance_maximum', $7::text)
		), updated AS (
			UPDATE settings SET value = input.value FROM input
			WHERE settings.settings = input.settings
			RETURNING settings.settings, settings.value
		)
		SELECT
			COALESCE(MAX(value) FILTER (WHERE settings = 'start_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'end_attendance'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'playtime_threshold'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'player_threshold'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'payment_contract'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'attendance_minimum'), ''),
			COALESCE(MAX(value) FILTER (WHERE settings = 'attendance_maximum'), ''), COUNT(*)
		FROM updated`
	var result Values
	var count int
	err = r.database.QueryRow(ctx, query, normalized.StartAttendance, normalized.EndAttendance, normalized.PlaytimeThreshold, normalized.PlayerThreshold, normalized.PaymentContract, normalized.AttendanceMinimum, normalized.AttendanceMaximum).Scan(
		&result.StartAttendance, &result.EndAttendance, &result.PlaytimeThreshold, &result.PlayerThreshold, &result.PaymentContract, &result.AttendanceMinimum, &result.AttendanceMaximum, &count,
	)
	if err != nil {
		return Values{}, fmt.Errorf("update settings: %w", err)
	}
	if count != 7 {
		return Values{}, fmt.Errorf("update settings: expected 7 rows, updated %d", count)
	}
	return result, nil
}

func Validate(values Values) (Values, error) {
	values.StartAttendance = strings.TrimSpace(values.StartAttendance)
	values.EndAttendance = strings.TrimSpace(values.EndAttendance)
	values.PlaytimeThreshold = strings.TrimSpace(values.PlaytimeThreshold)
	values.PlayerThreshold = strings.TrimSpace(values.PlayerThreshold)
	values.PaymentContract = strings.TrimSpace(values.PaymentContract)
	values.AttendanceMinimum = strings.TrimSpace(values.AttendanceMinimum)
	values.AttendanceMaximum = strings.TrimSpace(values.AttendanceMaximum)
	start, err := parseClockTime(values.StartAttendance)
	if err != nil {
		return Values{}, fmt.Errorf("setting start_attendance: %w", err)
	}
	end, err := parseClockTime(values.EndAttendance)
	if err != nil {
		return Values{}, fmt.Errorf("setting end_attendance: %w", err)
	}
	if start == end {
		return Values{}, errors.New("settings start_attendance and end_attendance must differ")
	}
	duration, err := time.ParseDuration(values.PlaytimeThreshold)
	if err != nil || duration <= 0 {
		return Values{}, errors.New("setting playtime_threshold must be a positive Go duration such as 90m or 1h30m")
	}
	threshold, err := strconv.Atoi(values.PlayerThreshold)
	if err != nil || threshold < 0 {
		return Values{}, errors.New("setting player_threshold must be a non-negative integer")
	}
	payment, err := strconv.ParseInt(values.PaymentContract, 10, 64)
	if err != nil || payment <= 0 {
		return Values{}, errors.New("setting payment_contract must be a positive integer")
	}
	minimum, err := strconv.Atoi(values.AttendanceMinimum)
	if err != nil || minimum < 1 || minimum > 31 {
		return Values{}, errors.New("setting attendance_minimum must be between 1 and 31 days")
	}
	maximum, err := strconv.Atoi(values.AttendanceMaximum)
	if err != nil || maximum < 1 || maximum > 31 {
		return Values{}, errors.New("setting attendance_maximum must be between 1 and 31 days")
	}
	if minimum > maximum {
		return Values{}, errors.New("setting attendance_minimum must not exceed attendance_maximum")
	}
	return values, nil
}

func (r *Repository) LoadAttendance(ctx context.Context) (Attendance, error) {
	values, err := r.Load(ctx)
	if err != nil {
		return Attendance{}, fmt.Errorf("load attendance settings: %w", err)
	}
	startTime, err := parseClockTime(values.StartAttendance)
	if err != nil {
		return Attendance{}, fmt.Errorf("setting start_attendance: %w", err)
	}
	endTime, err := parseClockTime(values.EndAttendance)
	if err != nil {
		return Attendance{}, fmt.Errorf("setting end_attendance: %w", err)
	}
	if startTime == endTime {
		return Attendance{}, errors.New("settings start_attendance and end_attendance must differ")
	}
	playtimeThreshold, err := time.ParseDuration(strings.TrimSpace(values.PlaytimeThreshold))
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
