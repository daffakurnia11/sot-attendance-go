package member

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Player struct {
	UserID           string
	Username         string
	DisplayName      string
	FirstConnectedAt time.Time
}

type PlayerLog struct {
	Player     Player
	Status     string
	StartedAt  *time.Time
	OccurredAt time.Time
	Playtime   *time.Duration
}

type PlaytimeRecap struct {
	MemberID      int64
	DisplayName   string
	CharacterName string
	Playtime      time.Duration
}

var ErrNotFound = errors.New("member not found")

type Member struct {
	ID            int64  `json:"id"`
	UserID        string `json:"discord_user_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	CharacterName string `json:"character_name"`
	IsAdmin       bool   `json:"is_admin"`
}

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (r *Repository) PlaytimeRecap(ctx context.Context, attendanceStart, attendanceEnd time.Time) ([]PlaytimeRecap, error) {
	const query = `
		WITH closed_sessions AS (
			SELECT member_id,
				SUM(EXTRACT(EPOCH FROM (LEAST(occurred_at, $2) - GREATEST(started_at, $1)))) AS seconds
			FROM player_logs
			WHERE status = 'disconnected'
				AND started_at IS NOT NULL
				AND occurred_at > $1
				AND occurred_at <= $2
				AND started_at < $2
			GROUP BY member_id
		), latest_logs AS (
			SELECT DISTINCT ON (member_id) member_id, status, started_at
			FROM player_logs
			WHERE occurred_at <= $2
			ORDER BY member_id, occurred_at DESC, id DESC
		), open_sessions AS (
			SELECT member_id,
				EXTRACT(EPOCH FROM ($2 - GREATEST(started_at, $1))) AS seconds
			FROM latest_logs
			WHERE status = 'connected'
				AND started_at IS NOT NULL
				AND started_at < $2
		), totals AS (
			SELECT member_id, SUM(seconds) AS seconds
			FROM (
				SELECT * FROM closed_sessions
				UNION ALL
				SELECT * FROM open_sessions
			) sessions
			GROUP BY member_id
		)
		SELECT m.id,
			m.display_name,
			COALESCE(NULLIF(m.character_name, ''), 'Unregistered'),
			FLOOR(t.seconds)::bigint
		FROM totals t
		JOIN members m ON m.id = t.member_id
		WHERE t.seconds > 0
		ORDER BY t.seconds DESC, m.display_name ASC`

	rows, err := r.database.Query(ctx, query, attendanceStart, attendanceEnd)
	if err != nil {
		return nil, fmt.Errorf("query playtime recap: %w", err)
	}
	defer rows.Close()

	recaps := make([]PlaytimeRecap, 0)
	for rows.Next() {
		var recap PlaytimeRecap
		var seconds int64
		if err := rows.Scan(&recap.MemberID, &recap.DisplayName, &recap.CharacterName, &seconds); err != nil {
			return nil, fmt.Errorf("scan playtime recap: %w", err)
		}
		recap.Playtime = time.Duration(seconds) * time.Second
		recaps = append(recaps, recap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate playtime recap: %w", err)
	}
	return recaps, nil
}

func (r *Repository) SaveAttendanceRecap(ctx context.Context, recaps []PlaytimeRecap, attendanceStart, attendanceEnd time.Time, requiredPlaytime time.Duration) error {
	if len(recaps) == 0 {
		return nil
	}
	memberIDs := make([]int64, len(recaps))
	playtimeSeconds := make([]int64, len(recaps))
	isAttended := make([]bool, len(recaps))
	for index, recap := range recaps {
		memberIDs[index] = recap.MemberID
		playtimeSeconds[index] = int64(recap.Playtime / time.Second)
		isAttended[index] = recap.Playtime > requiredPlaytime
	}

	const query = `
		INSERT INTO attendance_logs (
			member_id, attendance_start, attendance_end,
			playtime, required_playtime, is_attended
		)
		SELECT data.member_id, $4, $5,
			data.playtime_seconds * INTERVAL '1 second',
			$6::bigint * INTERVAL '1 second', data.is_attended
		FROM unnest($1::bigint[], $2::bigint[], $3::boolean[])
			AS data(member_id, playtime_seconds, is_attended)
		ON CONFLICT (member_id, attendance_start, attendance_end)
		DO UPDATE SET
			playtime = EXCLUDED.playtime,
			required_playtime = EXCLUDED.required_playtime,
			is_attended = EXCLUDED.is_attended`

	if _, err := r.database.Exec(ctx, query, memberIDs, playtimeSeconds, isAttended, attendanceStart, attendanceEnd, int64(requiredPlaytime/time.Second)); err != nil {
		return fmt.Errorf("save attendance recap: %w", err)
	}
	return nil
}

type Repository struct{ database executor }

func NewRepository(database executor) *Repository { return &Repository{database: database} }

func (r *Repository) SyncAdmins(ctx context.Context, adminUserIDs []string) error {
	const query = `
		UPDATE members
		SET is_admin = (user_id = ANY($1::text[])), updated_at = NOW()
		WHERE is_admin IS DISTINCT FROM (user_id = ANY($1::text[]))`
	if _, err := r.database.Exec(ctx, query, adminUserIDs); err != nil {
		return fmt.Errorf("sync member admins: %w", err)
	}
	return nil
}

// CloseOrphanedSessions records a disconnect for every member whose most
// recent log still reads connecting or connected but who is not in
// activeUserIDs.
//
// The gateway takes a presence baseline on startup without writing logs, so it
// cannot observe a disconnect that happened while the process was down. Without
// this, that member's latest row reads connected indefinitely and the dashboard
// keeps counting them as online.
//
// occurredAt stamps the reconciliation, not the real disconnect, which Discord
// does not report. Playtime is measured against it, so a session left open
// across a long outage is credited generously; across a deploy it is a matter
// of seconds.
func (r *Repository) CloseOrphanedSessions(ctx context.Context, activeUserIDs []string, occurredAt time.Time) (int64, error) {
	const query = `
		WITH latest AS (
			SELECT DISTINCT ON (member_id) member_id, status, started_at
			FROM player_logs
			ORDER BY member_id, occurred_at DESC, id DESC
		), orphaned AS (
			SELECT latest.member_id, latest.started_at
			FROM latest
			JOIN members ON members.id = latest.member_id
			WHERE latest.status IN ('connecting', 'connected')
				AND NOT (members.user_id = ANY($1::text[]))
		)
		INSERT INTO player_logs (member_id, status, started_at, occurred_at, playtime)
		SELECT member_id, 'disconnected', started_at, $2,
			CASE
				WHEN started_at IS NOT NULL AND $2 > started_at THEN $2 - started_at
				ELSE NULL
			END
		FROM orphaned`
	tag, err := r.database.Exec(ctx, query, activeUserIDs, occurredAt)
	if err != nil {
		return 0, fmt.Errorf("close orphaned player sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) UpsertGuildMembers(ctx context.Context, players []Player, observedAt time.Time) error {
	if len(players) == 0 {
		return nil
	}
	userIDs := make([]string, len(players))
	usernames := make([]string, len(players))
	displayNames := make([]string, len(players))
	for index, player := range players {
		userIDs[index] = player.UserID
		usernames[index] = player.Username
		displayNames[index] = player.DisplayName
	}
	const query = `
		INSERT INTO members (user_id, username, display_name, first_connected_at)
		SELECT data.user_id, data.username, data.display_name, $4
		FROM unnest($1::text[], $2::text[], $3::text[]) AS data(user_id, username, display_name)
		ON CONFLICT (user_id) DO UPDATE SET
			username = EXCLUDED.username,
			display_name = EXCLUDED.display_name,
			updated_at = NOW()`
	if _, err := r.database.Exec(ctx, query, userIDs, usernames, displayNames, observedAt); err != nil {
		return fmt.Errorf("upsert guild members: %w", err)
	}
	return nil
}

func (r *Repository) FindByUserID(ctx context.Context, userID string) (Member, error) {
	const query = `
		SELECT id, user_id, username, display_name, COALESCE(character_name, ''), is_admin
		FROM members
		WHERE user_id = $1`

	var found Member
	err := r.database.QueryRow(ctx, query, userID).Scan(
		&found.ID,
		&found.UserID,
		&found.Username,
		&found.DisplayName,
		&found.CharacterName,
		&found.IsAdmin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("find member by user ID: %w", err)
	}
	return found, nil
}

func (r *Repository) UpdateCharacterName(ctx context.Context, memberID int64, characterName string) (Member, error) {
	const query = `
		UPDATE members SET character_name = $2, updated_at = NOW()
		WHERE id = $1
		RETURNING id, user_id, username, display_name, character_name`
	var updated Member
	err := r.database.QueryRow(ctx, query, memberID, characterName).Scan(&updated.ID, &updated.UserID, &updated.Username, &updated.DisplayName, &updated.CharacterName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("update member character name: %w", err)
	}
	return updated, nil
}

func (r *Repository) RecordLog(ctx context.Context, log PlayerLog) error {
	const query = `
		WITH saved_member AS (
			INSERT INTO members (
			user_id, username, display_name, first_connected_at
			) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id) DO UPDATE SET
				username = EXCLUDED.username,
				display_name = EXCLUDED.display_name,
				updated_at = NOW()
			RETURNING id
		)
		INSERT INTO player_logs (
			member_id, status, started_at, occurred_at, playtime
		)
		SELECT id, $5, $6, $7, $8::bigint * INTERVAL '1 second'
		FROM saved_member`

	var playtimeSeconds *int64
	if log.Playtime != nil {
		seconds := int64(*log.Playtime / time.Second)
		playtimeSeconds = &seconds
	}

	if _, err := r.database.Exec(ctx, query,
		log.Player.UserID,
		log.Player.Username,
		log.Player.DisplayName,
		log.Player.FirstConnectedAt,
		log.Status,
		log.StartedAt,
		log.OccurredAt,
		playtimeSeconds,
	); err != nil {
		return fmt.Errorf("record player log: %w", err)
	}
	return nil
}
