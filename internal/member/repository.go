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
	DiscordUserID string
	Username      string
	DisplayName   string
}

type PlaytimeRecap struct {
	MemberID      int64
	DiscordUserID string
	DisplayName   string
	CharacterName string
	Playtime      time.Duration
}

var ErrNotFound = errors.New("member not found")

type Member struct {
	ID            int64  `json:"id"`
	DiscordUserID string `json:"discord_user_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	CharacterName string `json:"character_name"`
	CFXName       string `json:"cfx_name"`
	IsAdmin       bool   `json:"is_admin"`
}

// latestCharacter resolves the names that used to be members.character_name and
// members.cfx_name. Both come from server_members now, one row per character,
// so a member holding several has to be reduced to one: the most recently
// touched row, which is the character the game server saw last.
//
// Both names are the webhook's, not an operator's. A curated override lived
// here briefly and was dropped in 000030: beside player_name, reported on every
// event, it was the same value stored twice.
const latestCharacter = `
		LEFT JOIN LATERAL (
			SELECT sm.player_name AS character_name, sm.username
			FROM server_members sm
			WHERE sm.discord_user_id = m.discord_user_id
			ORDER BY sm.updated_at DESC, sm.id DESC
			LIMIT 1
		) latest_character ON TRUE`

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// visitMaxAge bounds how long a visit with no disconnected event is credited
// for. It mirrors the bound session resolution uses in internal/serverlog: no
// legitimate visit outlives the scheduled server restarts, so a visit whose
// disconnect never arrived is credited to its last observed event plus this,
// not to the end of time.
const visitMaxAge = 12 * time.Hour

// PlaytimeRecap totals each member's playtime inside an attendance window.
//
// Both feeds are read. server_logs - what the CR Roleplay server itself
// reported over the webhook - is the truth wherever it exists, and Discord rich
// presence is the fallback where it does not. Presence was only ever a guess at
// whether someone was in the game, and it both under- and over-reports: it lost
// one member's whole visit and cut another's short by an hour.
//
// The two are split at a per-member handover, that member's first webhook event
// ever. Presence accounts for the part of the window before it, the webhook for
// the part after. A member the game server has never reported has no handover
// and is measured entirely from presence, which is the fallback the rule exists
// for. Nothing is counted twice and nothing is dropped, which a plain
// preference between the sources could not manage.
//
// Playtime is the union of a member's visits, never their sum. Two visits can
// overlap - a member holding two characters, or a legacy session merged before
// session resolution was bounded - and a player is only ever in one place, so
// summing them credited one member 600 minutes inside a 300 minute window.
//
// A visit starts at its connected event, never at the connecting attempt, since
// a loading screen is not playtime. It ends at its disconnected event or, absent
// one, no later than visitMaxAge past its last event; an abandoned visit
// therefore ages out instead of counting forever. Both ends are clamped to the
// window.
func (r *Repository) PlaytimeRecap(ctx context.Context, attendanceStart, attendanceEnd time.Time) ([]PlaytimeRecap, error) {
	const query = `
		WITH handovers AS (
			SELECT sm.discord_user_id, MIN(sl.occurred_at) AS at
			FROM server_logs sl
			JOIN server_members sm ON sm.id = sl.server_member_id
			GROUP BY sm.discord_user_id
		), bounds AS (
			-- Where presence stops counting for this member. With no handover
			-- it is the window end, so presence covers the whole window.
			SELECT m.id AS member_id, m.discord_user_id,
				LEAST($2::timestamptz, COALESCE(h.at, $2::timestamptz)) AS presence_end
			FROM members m
			LEFT JOIN handovers h ON h.discord_user_id = m.discord_user_id
		), visits AS (
			SELECT sm.discord_user_id,
				MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS connected_at,
				MAX(sl.occurred_at) FILTER (WHERE sl.status = 'disconnected') AS disconnected_at,
				MAX(sl.occurred_at) AS last_event_at
			FROM server_logs sl
			JOIN server_members sm ON sm.id = sl.server_member_id
			GROUP BY sl.session_id, sm.discord_user_id
		), bounded AS (
			SELECT discord_user_id,
				GREATEST(connected_at, $1) AS starts,
				LEAST(COALESCE(disconnected_at, last_event_at + make_interval(secs => $3::double precision)), $2) AS ends
			FROM visits
			WHERE connected_at IS NOT NULL
				AND connected_at < $2
				AND COALESCE(disconnected_at, last_event_at + make_interval(secs => $3::double precision)) > $1
		), ordered AS (
			SELECT discord_user_id, starts, ends,
				MAX(ends) OVER (
					PARTITION BY discord_user_id ORDER BY starts, ends
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
				) AS prior_end
			FROM bounded
			WHERE ends > starts
		), islands AS (
			SELECT discord_user_id, starts, ends,
				SUM(CASE WHEN prior_end IS NULL OR starts > prior_end THEN 1 ELSE 0 END) OVER (
					PARTITION BY discord_user_id ORDER BY starts, ends
					ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
				) AS island
			FROM ordered
		), merged AS (
			SELECT discord_user_id, MIN(starts) AS starts, MAX(ends) AS ends
			FROM islands
			GROUP BY discord_user_id, island
		), server_seconds AS (
			SELECT b.member_id, SUM(EXTRACT(EPOCH FROM (merged.ends - merged.starts))) AS seconds
			FROM merged
			JOIN bounds b ON b.discord_user_id = merged.discord_user_id
			GROUP BY b.member_id
		), presence_closed AS (
			SELECT b.member_id,
				SUM(EXTRACT(EPOCH FROM (LEAST(a.occurred_at, b.presence_end) - GREATEST(a.started_at, $1)))) AS seconds
			FROM activity_logs a
			JOIN bounds b ON b.member_id = a.member_id
			WHERE a.status = 'disconnected'
				AND a.started_at IS NOT NULL
				AND LEAST(a.occurred_at, b.presence_end) > GREATEST(a.started_at, $1)
			GROUP BY b.member_id
		), presence_latest AS (
			SELECT DISTINCT ON (a.member_id) a.member_id, a.status, a.started_at
			FROM activity_logs a
			JOIN bounds b ON b.member_id = a.member_id
			WHERE a.occurred_at <= b.presence_end
			ORDER BY a.member_id, a.occurred_at DESC, a.id DESC
		), presence_open AS (
			SELECT l.member_id,
				EXTRACT(EPOCH FROM (b.presence_end - GREATEST(l.started_at, $1))) AS seconds
			FROM presence_latest l
			JOIN bounds b ON b.member_id = l.member_id
			WHERE l.status = 'connected'
				AND l.started_at IS NOT NULL
				AND b.presence_end > GREATEST(l.started_at, $1)
		), totals AS (
			SELECT member_id, SUM(seconds) AS seconds
			FROM (
				SELECT * FROM server_seconds
				UNION ALL
				SELECT * FROM presence_closed
				UNION ALL
				SELECT * FROM presence_open
			) sources
			GROUP BY member_id
		)
		SELECT m.id,
			m.discord_user_id,
			m.display_name,
			COALESCE(NULLIF(latest_character.character_name, ''), 'Unregistered'),
			FLOOR(t.seconds)::bigint
		FROM totals t
		JOIN members m ON m.id = t.member_id` + latestCharacter + `
		WHERE t.seconds > 0
		ORDER BY t.seconds DESC, m.display_name ASC`

	rows, err := r.database.Query(ctx, query, attendanceStart, attendanceEnd, visitMaxAge.Seconds())
	if err != nil {
		return nil, fmt.Errorf("query playtime recap: %w", err)
	}
	defer rows.Close()

	recaps := make([]PlaytimeRecap, 0)
	for rows.Next() {
		var recap PlaytimeRecap
		var seconds int64
		if err := rows.Scan(&recap.MemberID, &recap.DiscordUserID, &recap.DisplayName, &recap.CharacterName, &seconds); err != nil {
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
		SET is_admin = (discord_user_id = ANY($1::text[])), updated_at = NOW()
		WHERE is_admin IS DISTINCT FROM (discord_user_id = ANY($1::text[]))`
	if _, err := r.database.Exec(ctx, query, adminUserIDs); err != nil {
		return fmt.Errorf("sync member admins: %w", err)
	}
	return nil
}

func (r *Repository) UpsertGuildMembers(ctx context.Context, players []Player) error {
	if len(players) == 0 {
		return nil
	}
	discordUserIDs := make([]string, len(players))
	usernames := make([]string, len(players))
	displayNames := make([]string, len(players))
	for index, player := range players {
		discordUserIDs[index] = player.DiscordUserID
		usernames[index] = player.Username
		displayNames[index] = player.DisplayName
	}
	const query = `
		INSERT INTO members (discord_user_id, username, display_name)
		SELECT data.discord_user_id, data.username, data.display_name
		FROM unnest($1::text[], $2::text[], $3::text[]) AS data(discord_user_id, username, display_name)
		ON CONFLICT (discord_user_id) DO UPDATE SET
			username = EXCLUDED.username,
			display_name = EXCLUDED.display_name,
			updated_at = NOW()`
	if _, err := r.database.Exec(ctx, query, discordUserIDs, usernames, displayNames); err != nil {
		return fmt.Errorf("upsert guild members: %w", err)
	}
	return nil
}

func (r *Repository) FindByDiscordUserID(ctx context.Context, discordUserID string) (Member, error) {
	const query = `
		SELECT m.id, m.discord_user_id, m.username, m.display_name,
			COALESCE(latest_character.character_name, ''),
			COALESCE(latest_character.username, ''),
			m.is_admin
		FROM members m` + latestCharacter + `
		WHERE m.discord_user_id = $1`

	var found Member
	err := r.database.QueryRow(ctx, query, discordUserID).Scan(
		&found.ID,
		&found.DiscordUserID,
		&found.Username,
		&found.DisplayName,
		&found.CharacterName,
		&found.CFXName,
		&found.IsAdmin,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("find member by Discord user ID: %w", err)
	}
	return found, nil
}
