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
	// ServerMemberID names the character that earned this playtime, and CID its
	// character id. One member holding two characters produces two recap rows,
	// each judged against the threshold on its own: a player who logged 138
	// minutes on one and 115 on the other met it once and missed it once, which
	// a single row keyed on the member could not say.
	//
	// Both are zero for a member the game server has never reported, whose
	// playtime comes from the Discord presence fallback and belongs to no
	// character.
	ServerMemberID int64
	CID            string
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

// PlaytimeRecap lists playtime inside an attendance window, one row per
// character.
//
// A member holding two characters produces two rows, each judged against the
// threshold on its own. One player logged 138 minutes on one character and 115
// on another in the same window: separately that is one attendance met and one
// missed, which a single row keyed on the member could only report as 253
// minutes attended.
//
// Both feeds are read. server_logs - what the CR Roleplay server reported over
// the webhook - names the character, so it produces the character rows. Discord
// presence is the fallback for the period before a member's first webhook event
// ever, and belongs to no character, so it produces at most one extra row per
// member carrying their display name. A member the game server has never
// reported has only that row.
//
// Playtime is the union of a character's visits, never their sum, since a
// session merged before resolution was bounded can overlap another. A visit
// starts at its connected event, not the connecting attempt, and ends at its
// disconnect or no later than visitMaxAge past its last event. Both ends are
// clamped to the window.
func (r *Repository) PlaytimeRecap(ctx context.Context, attendanceStart, attendanceEnd time.Time) ([]PlaytimeRecap, error) {
	const query = `
		WITH handovers AS (
			SELECT sm.discord_user_id, MIN(sl.occurred_at) AS at
			FROM server_logs sl
			JOIN server_members sm ON sm.id = sl.server_member_id
			GROUP BY sm.discord_user_id
		), bounds AS (
			SELECT m.id AS member_id, m.discord_user_id,
				LEAST($2::timestamptz, COALESCE(h.at, $2::timestamptz)) AS presence_end
			FROM members m
			LEFT JOIN handovers h ON h.discord_user_id = m.discord_user_id
		), visits AS (
			SELECT sl.server_member_id,
				MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS connected_at,
				MAX(sl.occurred_at) FILTER (WHERE sl.status = 'disconnected') AS disconnected_at,
				MAX(sl.occurred_at) AS last_event_at
			FROM server_logs sl
			GROUP BY sl.session_id, sl.server_member_id
		), bounded AS (
			SELECT server_member_id,
				GREATEST(connected_at, $1) AS starts,
				LEAST(COALESCE(disconnected_at, last_event_at + make_interval(secs => $3::double precision)), $2) AS ends
			FROM visits
			WHERE connected_at IS NOT NULL
				AND connected_at < $2
				AND COALESCE(disconnected_at, last_event_at + make_interval(secs => $3::double precision)) > $1
		), ordered AS (
			SELECT server_member_id, starts, ends,
				MAX(ends) OVER (
					PARTITION BY server_member_id ORDER BY starts, ends
					ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING
				) AS prior_end
			FROM bounded
			WHERE ends > starts
		), islands AS (
			SELECT server_member_id, starts, ends,
				SUM(CASE WHEN prior_end IS NULL OR starts > prior_end THEN 1 ELSE 0 END) OVER (
					PARTITION BY server_member_id ORDER BY starts, ends
					ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
				) AS island
			FROM ordered
		), merged AS (
			SELECT server_member_id, MIN(starts) AS starts, MAX(ends) AS ends
			FROM islands
			GROUP BY server_member_id, island
		), character_seconds AS (
			SELECT server_member_id, SUM(EXTRACT(EPOCH FROM (ends - starts))) AS seconds
			FROM merged
			GROUP BY server_member_id
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
		), presence_seconds AS (
			SELECT member_id, SUM(seconds) AS seconds
			FROM (SELECT * FROM presence_closed UNION ALL SELECT * FROM presence_open) sources
			GROUP BY member_id
		)
		SELECT m.id, m.discord_user_id, m.display_name,
			sm.player_name, sm.id, sm.cid, FLOOR(c.seconds)::bigint
		FROM character_seconds c
		JOIN server_members sm ON sm.id = c.server_member_id
		JOIN members m ON m.discord_user_id = sm.discord_user_id
		WHERE c.seconds > 0
		UNION ALL
		SELECT m.id, m.discord_user_id, m.display_name,
			'Unregistered', 0, '', FLOOR(p.seconds)::bigint
		FROM presence_seconds p
		JOIN members m ON m.id = p.member_id
		WHERE p.seconds > 0
		ORDER BY 7 DESC, 4 ASC`

	rows, err := r.database.Query(ctx, query, attendanceStart, attendanceEnd, visitMaxAge.Seconds())
	if err != nil {
		return nil, fmt.Errorf("query playtime recap: %w", err)
	}
	defer rows.Close()

	recaps := make([]PlaytimeRecap, 0)
	for rows.Next() {
		var recap PlaytimeRecap
		var seconds int64
		if err := rows.Scan(&recap.MemberID, &recap.DiscordUserID, &recap.DisplayName,
			&recap.CharacterName, &recap.ServerMemberID, &recap.CID, &seconds); err != nil {
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

// SaveAttendanceRecap stores one row per character per window.
//
// The conflict target folds a null character to zero, matching the unique index
// 000032 created: a member the game server never reported has no character, and
// a plain unique constraint treats nulls as distinct, which would let the same
// window be written for them twice.
func (r *Repository) SaveAttendanceRecap(ctx context.Context, recaps []PlaytimeRecap, attendanceStart, attendanceEnd time.Time, requiredPlaytime time.Duration) error {
	if len(recaps) == 0 {
		return nil
	}
	memberIDs := make([]int64, len(recaps))
	serverMemberIDs := make([]*int64, len(recaps))
	playtimeSeconds := make([]int64, len(recaps))
	isAttended := make([]bool, len(recaps))
	for index, recap := range recaps {
		memberIDs[index] = recap.MemberID
		if recap.ServerMemberID != 0 {
			serverMemberID := recap.ServerMemberID
			serverMemberIDs[index] = &serverMemberID
		}
		playtimeSeconds[index] = int64(recap.Playtime / time.Second)
		isAttended[index] = recap.Playtime > requiredPlaytime
	}

	const query = `
		INSERT INTO attendance_logs (
			member_id, server_member_id, attendance_start, attendance_end,
			playtime, required_playtime, is_attended
		)
		SELECT data.member_id, data.server_member_id, $5, $6,
			data.playtime_seconds * INTERVAL '1 second',
			$7::bigint * INTERVAL '1 second', data.is_attended
		FROM unnest($1::bigint[], $2::bigint[], $3::bigint[], $4::boolean[])
			AS data(member_id, server_member_id, playtime_seconds, is_attended)
		ON CONFLICT (member_id, COALESCE(server_member_id, 0), attendance_start, attendance_end)
		DO UPDATE SET
			playtime = EXCLUDED.playtime,
			required_playtime = EXCLUDED.required_playtime,
			is_attended = EXCLUDED.is_attended`

	if _, err := r.database.Exec(ctx, query, memberIDs, serverMemberIDs, playtimeSeconds, isAttended, attendanceStart, attendanceEnd, int64(requiredPlaytime/time.Second)); err != nil {
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
