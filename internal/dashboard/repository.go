package dashboard

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Player struct {
	MemberID             int64      `json:"member_id"`
	Username             string     `json:"username"`
	DisplayName          string     `json:"display_name"`
	CharacterName        string     `json:"character_name"`
	StartedAt            *time.Time `json:"started_at"`
	Status               string     `json:"status"`
	TotalPlaytimeSeconds int64      `json:"total_playtime_seconds"`
}

type Snapshot struct {
	DiscordPlayers       []Player    `json:"discord_players"`
	PlayerThreshold      int         `json:"player_threshold"`
	TotalMembers         int         `json:"total_members"`
	TotalPlaytimeSeconds int64       `json:"total_playtime_seconds"`
	TotalAttended        int         `json:"total_attended"`
	TotalAttendances     int         `json:"total_attendances"`
	CFXPlayers           []CFXPlayer `json:"cfx_players"`
	CFXAvailable         bool        `json:"cfx_available"`
}

type PlayerLog struct {
	ID              int64      `json:"id"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at"`
	OccurredAt      time.Time  `json:"occurred_at"`
	PlaytimeSeconds *int64     `json:"playtime_seconds"`
}

type AttendanceLog struct {
	ID                      int64     `json:"id"`
	AttendanceStart         time.Time `json:"attendance_start"`
	AttendanceEnd           time.Time `json:"attendance_end"`
	PlaytimeSeconds         int64     `json:"playtime_seconds"`
	RequiredPlaytimeSeconds int64     `json:"required_playtime_seconds"`
	IsAttended              bool      `json:"is_attended"`
}

type MemberRecords struct {
	TotalPlaytimeSeconds int64           `json:"total_playtime_seconds"`
	TotalAttended        int             `json:"total_attended"`
	TotalAttendances     int             `json:"total_attendances"`
	PlayerLogs           []PlayerLog     `json:"player_logs"`
	AttendanceLogs       []AttendanceLog `json:"attendance_logs"`
}

type cfxPlayerReader interface {
	Players(context.Context) ([]CFXPlayer, error)
}
type Repository struct {
	database *pgxpool.Pool
	cfx      cfxPlayerReader
	logger   *slog.Logger
}

func NewRepository(database *pgxpool.Pool, cfx cfxPlayerReader, logger *slog.Logger) *Repository {
	return &Repository{database: database, cfx: cfx, logger: logger}
}

func (r *Repository) Get(ctx context.Context, memberID int64) (Snapshot, error) {
	var result Snapshot
	var thresholdValue string
	const summaryQuery = `
		SELECT
			(SELECT COUNT(*) FROM members),
			COALESCE((SELECT value FROM settings WHERE settings = 'player_threshold'), ''),
			COALESCE((SELECT SUM(EXTRACT(EPOCH FROM playtime))::bigint FROM player_logs WHERE member_id = $1 AND status = 'disconnected'), 0)
			+ COALESCE((
				SELECT EXTRACT(EPOCH FROM (NOW() - latest.started_at))::bigint
				FROM (
					SELECT status, started_at FROM player_logs
					WHERE member_id = $1 ORDER BY occurred_at DESC, id DESC LIMIT 1
				) latest
				WHERE latest.status = 'connected' AND latest.started_at IS NOT NULL
			), 0),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1 AND is_attended),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1)`
	if err := r.database.QueryRow(ctx, summaryQuery, memberID).Scan(
		&result.TotalMembers, &thresholdValue, &result.TotalPlaytimeSeconds,
		&result.TotalAttended, &result.TotalAttendances,
	); err != nil {
		return Snapshot{}, fmt.Errorf("query dashboard summary: %w", err)
	}
	threshold, err := strconv.Atoi(thresholdValue)
	if err != nil || threshold < 0 {
		return Snapshot{}, fmt.Errorf("setting player_threshold must be a non-negative integer")
	}
	result.PlayerThreshold = threshold

	const playersQuery = `
		WITH latest AS (
			SELECT DISTINCT ON (member_id) member_id, status, started_at
			FROM player_logs
			ORDER BY member_id, occurred_at DESC, id DESC
		)
		SELECT
			m.id,
			m.username,
			m.display_name,
			COALESCE(m.character_name, ''),
			latest.started_at,
			CASE
				WHEN latest.status IN ('connecting', 'connected') THEN latest.status
				ELSE 'offline'
			END,
			COALESCE((
				SELECT SUM(EXTRACT(EPOCH FROM logs.playtime))::bigint
				FROM player_logs logs
				WHERE logs.member_id = m.id AND logs.status = 'disconnected'
			), 0) + CASE
				WHEN latest.status = 'connected' AND latest.started_at IS NOT NULL
				THEN GREATEST(EXTRACT(EPOCH FROM (NOW() - latest.started_at))::bigint, 0)
				ELSE 0
			END
		FROM members m
		LEFT JOIN latest ON latest.member_id = m.id
		ORDER BY m.display_name, m.id`
	rows, err := r.database.Query(ctx, playersQuery)
	if err != nil {
		return Snapshot{}, fmt.Errorf("query current Discord players: %w", err)
	}
	defer rows.Close()
	result.DiscordPlayers = make([]Player, 0)
	for rows.Next() {
		var player Player
		if err := rows.Scan(&player.MemberID, &player.Username, &player.DisplayName, &player.CharacterName, &player.StartedAt, &player.Status, &player.TotalPlaytimeSeconds); err != nil {
			return Snapshot{}, fmt.Errorf("scan Discord player: %w", err)
		}
		result.DiscordPlayers = append(result.DiscordPlayers, player)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("iterate current Discord players: %w", err)
	}
	result.CFXPlayers, err = r.cfx.Players(ctx)
	if err != nil {
		r.logger.Warn("CFX players unavailable", "error", err)
		result.CFXPlayers = make([]CFXPlayer, 0)
		return result, nil
	}
	result.CFXAvailable = true
	return result, nil
}

func (r *Repository) GetMemberRecords(ctx context.Context, memberID int64) (MemberRecords, error) {
	var result MemberRecords
	const summaryQuery = `
		SELECT
			COALESCE((SELECT SUM(EXTRACT(EPOCH FROM playtime))::bigint FROM player_logs WHERE member_id = $1 AND status = 'disconnected'), 0)
			+ COALESCE((SELECT EXTRACT(EPOCH FROM (NOW() - latest.started_at))::bigint FROM (SELECT status, started_at FROM player_logs WHERE member_id = $1 ORDER BY occurred_at DESC, id DESC LIMIT 1) latest WHERE latest.status = 'connected' AND latest.started_at IS NOT NULL), 0),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1 AND is_attended),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1)`
	if err := r.database.QueryRow(ctx, summaryQuery, memberID).Scan(&result.TotalPlaytimeSeconds, &result.TotalAttended, &result.TotalAttendances); err != nil {
		return MemberRecords{}, fmt.Errorf("query member records summary: %w", err)
	}

	const playerLogsQuery = `
		SELECT id, status, started_at, occurred_at,
			CASE WHEN playtime IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM playtime)::bigint END
		FROM player_logs WHERE member_id = $1 ORDER BY occurred_at DESC, id DESC`
	rows, err := r.database.Query(ctx, playerLogsQuery, memberID)
	if err != nil {
		return MemberRecords{}, fmt.Errorf("query member player logs: %w", err)
	}
	result.PlayerLogs = make([]PlayerLog, 0)
	for rows.Next() {
		var record PlayerLog
		if err := rows.Scan(&record.ID, &record.Status, &record.StartedAt, &record.OccurredAt, &record.PlaytimeSeconds); err != nil {
			rows.Close()
			return MemberRecords{}, fmt.Errorf("scan member player log: %w", err)
		}
		result.PlayerLogs = append(result.PlayerLogs, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MemberRecords{}, fmt.Errorf("iterate member player logs: %w", err)
	}
	rows.Close()

	const attendanceLogsQuery = `
		SELECT id, attendance_start, attendance_end,
			EXTRACT(EPOCH FROM playtime)::bigint,
			EXTRACT(EPOCH FROM required_playtime)::bigint, is_attended
		FROM attendance_logs WHERE member_id = $1 ORDER BY attendance_start DESC, id DESC`
	attendanceRows, err := r.database.Query(ctx, attendanceLogsQuery, memberID)
	if err != nil {
		return MemberRecords{}, fmt.Errorf("query member attendance logs: %w", err)
	}
	defer attendanceRows.Close()
	result.AttendanceLogs = make([]AttendanceLog, 0)
	for attendanceRows.Next() {
		var record AttendanceLog
		if err := attendanceRows.Scan(&record.ID, &record.AttendanceStart, &record.AttendanceEnd, &record.PlaytimeSeconds, &record.RequiredPlaytimeSeconds, &record.IsAttended); err != nil {
			return MemberRecords{}, fmt.Errorf("scan member attendance log: %w", err)
		}
		result.AttendanceLogs = append(result.AttendanceLogs, record)
	}
	if err := attendanceRows.Err(); err != nil {
		return MemberRecords{}, fmt.Errorf("iterate member attendance logs: %w", err)
	}
	return result, nil
}
