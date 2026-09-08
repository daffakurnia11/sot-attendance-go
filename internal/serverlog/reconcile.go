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
			MAX(sl.occurred_at) AS last_event_at
		FROM server_logs sl
		JOIN server_members sm ON sm.id = sl.server_member_id
		GROUP BY sl.session_id, sl.server_member_id, sm.username, sm.player_name, sm.cid
		HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0
			AND COUNT(*) FILTER (WHERE sl.status = 'connected') > 0
	), absent AS (
		SELECT * FROM open_visits
		WHERE last_event_at < $2::timestamptz - make_interval(secs => $3::double precision)
			AND LOWER(BTRIM(username)) <> ALL($1::text[])
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

// CloseAbsentSessions closes open visits for players missing from the roster.
//
// presentNames is every player the game server currently reports, matched
// against server_members.username the same way the player log matches them:
// trimmed and case-insensitive.
//
// An empty roster closes nothing. A failed or empty upstream read is not
// evidence that the server emptied, and acting on it would end every open visit
// at once - the same reasoning that stops an empty admin list demoting the
// roster.
func (r *Repository) CloseAbsentSessions(ctx context.Context, presentNames []string, observedAt time.Time, grace time.Duration) (int64, error) {
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

	rows, err := r.pool.Query(ctx, closeAbsentSessions, normalized, observedAt, grace.Seconds())
	if err != nil {
		return 0, fmt.Errorf("close absent server sessions: %w", err)
	}
	defer rows.Close()

	var closed int64
	for rows.Next() {
		closed++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read closed server sessions: %w", err)
	}
	return closed, nil
}
