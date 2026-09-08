package serverlog

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReconcileGrace is how long a visit is left alone before the CFX roster is
// allowed to contradict it.
//
// The roster is polled, so a player who has just connected is legitimately
// absent from it for up to one poll interval - that gap is what the player log
// shows as "polling". Closing a visit inside it would end a session that had
// only just begun.
const ReconcileGrace = 4 * time.Minute

// MaxClosedShare caps how much of the open population one sweep may end.
//
// A partial roster looks exactly like a mass exodus: the CFX read is a third
// party HTTP call that can answer 200 with a truncated player list, and every
// player missing from it would be closed at once. Real departures trickle, so a
// sweep that would end more than this share of open visits is refused as
// evidence of a bad read rather than obeyed.
const MaxClosedShare = 0.5

// MinClosedSessions is what a sweep may always close, whatever the share.
//
// The share alone is unusable on a quiet server: with one open visit, closing
// it is 100% of the population, so the cap would refuse the exact case the
// sweep exists for. Lost disconnects arrive in ones and twos, so that many are
// allowed outright and the share only governs anything larger.
const MinClosedSessions = 2

// closeAbsentSessions ends every open visit whose player the game server no
// longer lists.
//
// A visit stays open until a disconnected event arrives, and that event does go
// missing: a crash, a lost sender queue, a resource that never fired. The visit
// then reads as connected until it ages out twelve hours later, and attendance
// credits the player for the whole window. Two sessions were open for seven and
// eight hours the morning this was written, both for players the roster had
// already dropped, and one had banked a full five-hour attendance window.
//
// The CFX roster is the same source the bot already polls for its status count,
// and it answers exactly the question the missing event would have: is this
// player still on the server?
const closeAbsentSessions = `
	WITH open_visits AS (
		SELECT sl.session_id, sl.server_member_id, sm.username, sm.player_name, sm.cid,
			sm.discord_user_id,
			MAX(sl.occurred_at) AS last_event_at
		FROM server_logs sl
		JOIN server_members sm ON sm.id = sl.server_member_id
		GROUP BY sl.session_id, sl.server_member_id, sm.username, sm.player_name, sm.cid, sm.discord_user_id
		HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0
			AND COUNT(*) FILTER (WHERE sl.status = 'connected') > 0
	), absent AS (
		SELECT * FROM open_visits
		WHERE last_event_at < $2::timestamptz - make_interval(secs => $3::double precision)
			AND LOWER(BTRIM(username)) <> ALL($1::text[])
			-- Both pollers have to agree. Discord rich presence is the second
			-- opinion: a member whose activity still names the server is on it,
			-- whatever a truncated CFX read says.
			AND (discord_user_id IS NULL OR discord_user_id <> ALL($4::text[]))
	)
	INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
	SELECT jsonb_build_object(
			'event', jsonb_build_object(
				'type', 'disconnected',
				'reason', 'Reconciled: absent from the CFX roster',
				'timestamp', to_char($2::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
			),
			'player', jsonb_build_object(
				'name', player_name,
				'cid', cid,
				'username', username,
				'session_id', session_id::text
			)
		),
		server_member_id, session_id, 'disconnected', $2
	FROM absent
	ON CONFLICT (payload) DO NOTHING
	RETURNING session_id`

const countOpenVisits = `
	SELECT COUNT(*) FROM (
		SELECT sl.session_id
		FROM server_logs sl
		GROUP BY sl.session_id
		HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0
			AND COUNT(*) FILTER (WHERE sl.status = 'connected') > 0
	) open_visits`

// CloseAbsentSessions closes open visits for players missing from the roster.
//
// The webhook stays the authority: it opens visits, and a disconnected event it
// sends closes them outright. The two pollers only get to agree that a player is
// gone, and both must, because each is wrong in its own way - the CFX read can
// come back truncated, and Discord presence cannot see an invisible member.
//
// presentNames is every player the game server currently reports, matched
// against server_members.username the same way the player log matches them:
// trimmed and case-insensitive. playingDiscordUserIDs is every member whose
// Discord activity currently names the server.
//
// An empty roster closes nothing. A failed or empty upstream read is not
// evidence that the server emptied, and acting on it would end every open visit
// at once - the same reasoning that stops an empty admin list demoting the
// roster. Missing evidence never closes a visit; only agreement does.
func (r *Repository) CloseAbsentSessions(ctx context.Context, presentNames, playingDiscordUserIDs []string, observedAt time.Time, grace time.Duration) (int64, error) {
	if len(presentNames) == 0 {
		return 0, nil
	}
	normalized := make([]string, 0, len(presentNames))
	for _, name := range presentNames {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		return 0, nil
	}

	playing := playingDiscordUserIDs
	if playing == nil {
		playing = []string{}
	}

	// The cap needs the population before the writes, and a refusal has to undo
	// them, so both happen in one transaction.
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin absent session sweep: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var open int64
	if err := transaction.QueryRow(ctx, countOpenVisits).Scan(&open); err != nil {
		return 0, fmt.Errorf("count open server sessions: %w", err)
	}

	rows, err := transaction.Query(ctx, closeAbsentSessions, normalized, observedAt, grace.Seconds(), playing)
	if err != nil {
		return 0, fmt.Errorf("close absent server sessions: %w", err)
	}
	var closed int64
	for rows.Next() {
		closed++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read closed server sessions: %w", err)
	}
	rows.Close()

	allowed := float64(open) * MaxClosedShare
	if allowed < MinClosedSessions {
		allowed = MinClosedSessions
	}
	if float64(closed) > allowed {
		return 0, fmt.Errorf("refusing to close %d of %d open server sessions: the roster read looks incomplete", closed, open)
	}
	if err := transaction.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit absent session sweep: %w", err)
	}
	return closed, nil
}
