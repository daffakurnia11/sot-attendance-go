package serverlog

import (
	"context"
	"fmt"
	"time"
)

// ReconcileGrace is the floor on a visit's age before the sweep may consider
// it at all, whatever the pollers have seen.
//
// The caller's idle timeout is the real control. This only stops a visit being
// closed in the minutes after it opens, before any poller has had the chance
// to report the player even once.
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
		-- Only the webhook's own visits. This sweep exists to supply the
		-- disconnected event the game server failed to send, so a visit it
		-- never opened is none of its business: a Discord visit is closed by
		-- the poller that opened it, and closing one here wrote a third exit
		-- for a player, labelled as though the game server had reported it.
		WHERE sl.source = 'server'
		GROUP BY sl.session_id, sl.server_member_id, sm.username, sm.player_name, sm.cid, sm.discord_user_id
		HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0
			AND COUNT(*) FILTER (WHERE sl.status = 'connected') > 0
	), absent AS (
		SELECT * FROM open_visits
		WHERE last_event_at < $2::timestamptz - make_interval(secs => $3::double precision)
			-- Neither list is a claim that anybody left. Both are lists of
			-- players a source positively placed on the server inside the idle
			-- window, so a name in either one keeps its visit alive.
			AND LOWER(BTRIM(username)) <> ALL($1::text[])
			AND (discord_user_id IS NULL OR discord_user_id <> ALL($4::text[]))
			-- And CFX has placed them there at least once. A player it has
			-- never listed - because it lagged behind their connect, or because
			-- their in-game name never matched - cannot be absent from a list
			-- they were never on, so their visit is left to the twelve hour cap.
			AND LOWER(BTRIM(username)) = ANY($5::text[])
	)
	INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
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
		server_member_id, session_id, 'disconnected', $2, 'server'
	FROM absent
	ON CONFLICT (payload) DO NOTHING
	RETURNING session_id`

// Counted over the same population the sweep may act on, so the share cap is
// measured against webhook visits rather than diluted by Discord ones.
const countOpenVisits = `
	SELECT COUNT(*) FROM (
		SELECT sl.session_id
		FROM server_logs sl
		WHERE sl.source = 'server'
		GROUP BY sl.session_id
		HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0
			AND COUNT(*) FILTER (WHERE sl.status = 'connected') > 0
	) open_visits`

// CloseIdleSessions closes open visits no source has seen for the idle window.
//
// The webhook stays the authority: it opens visits, and a disconnected event it
// sends closes them outright. No other source may claim a player left. The two
// pollers only report players they have seen, and any sighting inside the
// window keeps a visit alive, because each poller is blind in its own way - a
// CFX read can come back truncated or lag behind a connect, and Discord
// presence cannot see an invisible member.
//
// Only visits the webhook opened are eligible. This is the backup for an exit
// event that never arrived, so a visit that already has one is untouched, and
// so is a visit from any other source.
//
// seenRecentlyNames is every player a source placed on the server inside the
// idle window, and seenEverNames every player it has placed there at all, both
// matched against server_members.username the way the player log matches them:
// trimmed and case-insensitive. playingDiscordUserIDs is every member whose
// Discord activity currently names the server.
//
// seenEverNames is the guard against the connect-lag case: a player CFX has
// never listed cannot be absent from a list they were never on, so their visit
// is left to the twelve hour cap rather than closed minutes after it opened.
//
// Nothing seen recently closes nothing. A failed or empty upstream read looks
// exactly like an emptied server, and acting on it would end every open visit
// at once - the same reasoning that stops an empty admin list demoting the
// roster. Missing evidence never closes a visit; only a sighting elsewhere,
// long enough ago, does.
func (r *Repository) CloseIdleSessions(ctx context.Context, seenRecentlyNames, seenEverNames, playingDiscordUserIDs []string, observedAt time.Time, idle time.Duration) (int64, error) {
	// No player has ever been seen, so there is no baseline to be absent from
	// and nothing can be judged idle. A freshly started process sits here until
	// its first healthy roster read.
	everSeen := lowered(seenEverNames)
	if len(everSeen) == 0 {
		return 0, nil
	}
	// Nobody has been seen inside the idle window. That is what a failed or
	// empty upstream read looks like, and it is indistinguishable from the
	// server genuinely emptying, so it closes nothing: missing evidence never
	// ends a visit. A truly empty server ages out on the twelve hour cap.
	normalized := lowered(seenRecentlyNames)
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

	rows, err := transaction.Query(ctx, closeAbsentSessions, normalized, observedAt, idle.Seconds(), playing, everSeen)
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

// lowered trims and lowercases, dropping blanks, so a roster name matches
// server_members.username the same way the player log matches it.
func lowered(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := normalizedName(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
