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
	MemberID int64 `json:"member_id"`
	// DiscordUserID keys the live presence overlay, and is what the bot and
	// server_members both identify a member by.
	DiscordUserID string     `json:"discord_user_id"`
	Username      string     `json:"username"`
	DisplayName   string     `json:"display_name"`
	CharacterName string     `json:"character_name"`
	CFXName       string     `json:"cfx_name"`
	StartedAt     *time.Time `json:"started_at"`
	// Status is the playtime-bearing status: the game server's where it has
	// reported this member, Discord presence where it has not.
	Status string `json:"status"`
	// DiscordStatus is live Discord presence, pulled from the bot per request
	// and stored nowhere: one of online, idle, dnd, offline, invisible, or
	// "unknown" when the bot could not be reached or has nothing on this
	// member. DiscordPlaying reports whether their activity matches the
	// configured server name. A reader showing a Discord column wants these,
	// not Status.
	DiscordStatus          string `json:"discord_status"`
	DiscordPlaying         bool   `json:"discord_playing"`
	CurrentPlaytimeSeconds int64  `json:"current_playtime_seconds"`
	TotalPlaytimeSeconds   int64  `json:"total_playtime_seconds"`
}

type Snapshot struct {
	DiscordPlayers       []Player    `json:"discord_players"`
	PlayerThreshold      int         `json:"player_threshold"`
	TotalMembers         int         `json:"total_members"`
	TotalPlaytimeSeconds int64       `json:"total_playtime_seconds"`
	TotalAttended        int         `json:"total_attended"`
	TotalAttendances     int         `json:"total_attendances"`
	CFXPlayers           []CFXPlayer `json:"cfx_players"`
	AllCFXPlayers        []CFXPlayer `json:"all_cfx_players"`
	CFXAvailable         bool        `json:"cfx_available"`
	// DiscordPresenceAvailable reports whether live presence reached us. False
	// leaves every DiscordStatus as "unknown", which is the honest answer: the
	// bot holds the gateway and nothing persists presence any more.
	DiscordPresenceAvailable bool `json:"discord_presence_available"`
}

type PlayerLog struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	// Source names the feed the row came from: "fivem" for what the CR
	// Roleplay server reported over the webhook, "discord" for rich presence
	// watched through the gateway. Ids are only unique within a source, so a
	// reader keying rows needs both.
	Source          string     `json:"source"`
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
	Rosters(context.Context) ([]CFXPlayer, []CFXPlayer, error)
}
type presenceReader interface {
	Presences(context.Context) (map[string]MemberPresence, error)
}

type Repository struct {
	database *pgxpool.Pool
	cfx      cfxPlayerReader
	presence presenceReader
	logger   *slog.Logger
}

func NewRepository(database *pgxpool.Pool, cfx cfxPlayerReader, logger *slog.Logger) *Repository {
	return &Repository{database: database, cfx: cfx, logger: logger}
}

// WithPresence attaches a live Discord presence source. Without one the
// dashboard still answers, reporting every Discord status as unknown.
func (r *Repository) WithPresence(presence presenceReader) *Repository {
	r.presence = presence
	return r
}

func (r *Repository) Get(ctx context.Context, memberID int64) (Snapshot, error) {
	var result Snapshot
	var thresholdValue string
	const summaryQuery = `
		SELECT
			(SELECT COUNT(*) FROM members),
			COALESCE((SELECT value FROM settings WHERE settings = 'player_threshold'), ''),
			COALESCE((SELECT SUM(EXTRACT(EPOCH FROM playtime))::bigint FROM activity_logs WHERE member_id = $1 AND status = 'disconnected'), 0)
			+ COALESCE((
				SELECT EXTRACT(EPOCH FROM (NOW() - latest.started_at))::bigint
				FROM (
					SELECT status, started_at FROM activity_logs
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

	// Both feeds again, on the same rule as the member records: server_logs is
	// the truth where it exists, presence is the fallback where it does not,
	// and the two are split at each member's first webhook event.
	//
	// A webhook status also ages out. A visit whose disconnect never arrived
	// would otherwise read connected forever, which is the failure the removed
	// server_members.last_status cache had.
	const playersQuery = `
		WITH handovers AS (
			SELECT sm.discord_user_id, MIN(sl.occurred_at) AS at
			FROM server_logs sl
			JOIN server_members sm ON sm.id = sl.server_member_id
			GROUP BY sm.discord_user_id
		), bounds AS (
			SELECT m.id AS member_id, m.discord_user_id,
				COALESCE(h.at, NOW()) AS presence_end
			FROM members m
			LEFT JOIN handovers h ON h.discord_user_id = m.discord_user_id
		), visits AS (
			SELECT sm.discord_user_id, sl.session_id,
				MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS connected_at,
				MAX(sl.occurred_at) FILTER (WHERE sl.status = 'disconnected') AS disconnected_at,
				MAX(sl.occurred_at) AS last_event_at
			FROM server_logs sl
			JOIN server_members sm ON sm.id = sl.server_member_id
			GROUP BY sm.discord_user_id, sl.session_id
		), bounded AS (
			SELECT discord_user_id, connected_at AS starts,
				LEAST(COALESCE(disconnected_at, last_event_at + make_interval(secs => $1::double precision)), NOW()) AS ends,
				disconnected_at, last_event_at
			FROM visits
			WHERE connected_at IS NOT NULL
		), latest_visit AS (
			SELECT DISTINCT ON (discord_user_id) discord_user_id, starts, ends, disconnected_at, last_event_at
			FROM bounded
			ORDER BY discord_user_id, last_event_at DESC
		), server_status AS (
			-- The bound has to be read unclamped here: bounded.ends is capped
			-- at NOW(), so comparing it to NOW() could never be true.
			SELECT discord_user_id,
				CASE
					WHEN disconnected_at IS NULL
						AND last_event_at + make_interval(secs => $1::double precision) > NOW()
					THEN 'connected'
					ELSE 'offline'
				END AS status,
				CASE
					WHEN disconnected_at IS NULL
						AND last_event_at + make_interval(secs => $1::double precision) > NOW()
					THEN starts
				END AS started_at
			FROM latest_visit
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
			SELECT discord_user_id, SUM(EXTRACT(EPOCH FROM (ends - starts)))::bigint AS seconds
			FROM merged
			GROUP BY discord_user_id
		), presence_latest AS (
			SELECT DISTINCT ON (a.member_id) a.member_id, a.status, a.started_at
			FROM activity_logs a
			ORDER BY a.member_id, a.occurred_at DESC, a.id DESC
		), presence_seconds AS (
			SELECT b.member_id,
				SUM(EXTRACT(EPOCH FROM a.playtime))::bigint AS seconds
			FROM activity_logs a
			JOIN bounds b ON b.member_id = a.member_id
			WHERE a.status = 'disconnected'
				AND a.playtime IS NOT NULL
				AND a.occurred_at < b.presence_end
			GROUP BY b.member_id
		)
		SELECT
			m.id,
			m.discord_user_id,
			m.username,
			m.display_name,
			COALESCE(latest_character.character_name, ''),
			COALESCE(latest_character.username, ''),
			COALESCE(server_status.started_at, presence_started.started_at) AS started_at,
			COALESCE(
				NULLIF(server_status.status, 'offline'),
				CASE
					WHEN server_status.status IS NULL AND presence_latest.status IN ('connecting', 'connected')
					THEN presence_latest.status
				END,
				'offline'
			) AS status,
			CASE
				WHEN COALESCE(server_status.started_at, presence_started.started_at) IS NOT NULL
				THEN GREATEST(EXTRACT(EPOCH FROM (NOW() - COALESCE(server_status.started_at, presence_started.started_at)))::bigint, 0)
				ELSE 0
			END AS current_playtime,
			COALESCE(server_seconds.seconds, 0) + COALESCE(presence_seconds.seconds, 0) AS total_playtime
		FROM members m
		LEFT JOIN bounds ON bounds.member_id = m.id
		LEFT JOIN server_status ON server_status.discord_user_id = m.discord_user_id
		LEFT JOIN server_seconds ON server_seconds.discord_user_id = m.discord_user_id
		LEFT JOIN presence_latest ON presence_latest.member_id = m.id
		LEFT JOIN presence_seconds ON presence_seconds.member_id = m.id
		LEFT JOIN LATERAL (
			-- Presence supplies a start only where the webhook has no say.
			SELECT presence_latest.started_at AS started_at
			WHERE server_status.status IS NULL
				AND presence_latest.status IN ('connecting', 'connected')
				AND presence_latest.started_at IS NOT NULL
		) presence_started ON TRUE
		LEFT JOIN LATERAL (
			SELECT sm.player_name AS character_name, sm.username
			FROM server_members sm
			WHERE sm.discord_user_id = m.discord_user_id
			ORDER BY sm.updated_at DESC, sm.id DESC
			LIMIT 1
		) latest_character ON TRUE
		ORDER BY m.display_name, m.id`
	rows, err := r.database.Query(ctx, playersQuery, visitMaxAge.Seconds())
	if err != nil {
		return Snapshot{}, fmt.Errorf("query current Discord players: %w", err)
	}
	defer rows.Close()
	result.DiscordPlayers = make([]Player, 0)
	for rows.Next() {
		var player Player
		if err := rows.Scan(&player.MemberID, &player.DiscordUserID, &player.Username, &player.DisplayName, &player.CharacterName, &player.CFXName, &player.StartedAt, &player.Status, &player.CurrentPlaytimeSeconds, &player.TotalPlaytimeSeconds); err != nil {
			return Snapshot{}, fmt.Errorf("scan Discord player: %w", err)
		}
		result.DiscordPlayers = append(result.DiscordPlayers, player)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("iterate current Discord players: %w", err)
	}

	// Live presence is an overlay, not a join: it comes from the bot's gateway
	// cache over HTTP and is deliberately never stored, so a failure here costs
	// the Discord column and nothing else.
	presences, presenceErr := r.livePresences(ctx)
	if presenceErr != nil {
		r.logger.Warn("Discord presence unavailable", "error", presenceErr)
	}
	result.DiscordPresenceAvailable = presenceErr == nil && presences != nil
	for index := range result.DiscordPlayers {
		result.DiscordPlayers[index].DiscordStatus = "unknown"
		if !result.DiscordPresenceAvailable {
			continue
		}
		if entry, found := presences[result.DiscordPlayers[index].DiscordUserID]; found {
			result.DiscordPlayers[index].DiscordStatus = entry.Status
			result.DiscordPlayers[index].DiscordPlaying = entry.Playing
		}
	}

	result.CFXPlayers, result.AllCFXPlayers, err = r.cfx.Rosters(ctx)
	if err != nil {
		r.logger.Warn("CFX players unavailable", "error", err)
		result.CFXPlayers = make([]CFXPlayer, 0)
		result.AllCFXPlayers = make([]CFXPlayer, 0)
		return result, nil
	}
	result.CFXAvailable = true
	return result, nil
}

// visitMaxAge bounds a visit with no disconnected event, matching the bound
// session resolution and the attendance recap use. A visit whose disconnect
// never arrived is credited to its last observed event plus this, not forever.
const visitMaxAge = 12 * time.Hour

// GetMemberRecords reads both feeds.
//
// server_logs is the truth where it exists: it is what the game server itself
// reported. It only starts when a member's first webhook event landed, though,
// and members the server has never reported have none at all - so activity_logs
// remains the fallback, and the handover is that member's own first webhook
// event rather than one global date.
//
// Totals are therefore split at that per-member cutover: Discord presence
// accounts for everything before it, the webhook for everything after. Nothing
// is counted twice and nothing is dropped, which a plain preference between the
// two sources could not manage - it would either lose the history or double the
// overlap.
//
// The log list is not split. It carries every row from both feeds, tagged with
// its source, since the list is a record of what each feed observed and the two
// disagreeing is worth seeing.
func (r *Repository) livePresences(ctx context.Context) (map[string]MemberPresence, error) {
	if r.presence == nil {
		return nil, nil
	}
	return r.presence.Presences(ctx)
}

func (r *Repository) GetMemberRecords(ctx context.Context, memberID int64) (MemberRecords, error) {
	var result MemberRecords
	const summaryQuery = `
		WITH characters AS (
			SELECT sm.id
			FROM server_members sm
			JOIN members m ON m.discord_user_id = sm.discord_user_id
			WHERE m.id = $1
		), cutover AS (
			-- The member's first webhook event. NULL when the game server has
			-- never reported them, which hands the whole total to Discord.
			SELECT MIN(sl.occurred_at) AS at
			FROM server_logs sl
			WHERE sl.server_member_id IN (SELECT id FROM characters)
		), visits AS (
			SELECT MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS starts,
				MAX(sl.occurred_at) FILTER (WHERE sl.status = 'disconnected') AS disconnected_at,
				MAX(sl.occurred_at) AS last_event_at
			FROM server_logs sl
			WHERE sl.server_member_id IN (SELECT id FROM characters)
			GROUP BY sl.session_id
		), bounded AS (
			SELECT starts,
				LEAST(COALESCE(disconnected_at, last_event_at + make_interval(secs => $2::double precision)), NOW()) AS ends
			FROM visits
			WHERE starts IS NOT NULL
		), ordered AS (
			SELECT starts, ends,
				MAX(ends) OVER (ORDER BY starts, ends ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) AS prior_end
			FROM bounded
			WHERE ends > starts
		), islands AS (
			SELECT starts, ends,
				SUM(CASE WHEN prior_end IS NULL OR starts > prior_end THEN 1 ELSE 0 END) OVER (
					ORDER BY starts, ends ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
				) AS island
			FROM ordered
		), merged AS (
			-- Overlapping visits collapse first: two characters, or a session
			-- merged before resolution was bounded, must not count twice.
			SELECT MIN(starts) AS starts, MAX(ends) AS ends
			FROM islands
			GROUP BY island
		), server_seconds AS (
			SELECT COALESCE(SUM(EXTRACT(EPOCH FROM (ends - starts))), 0)::bigint AS seconds
			FROM merged
		), discord_seconds AS (
			SELECT COALESCE((
				SELECT SUM(EXTRACT(EPOCH FROM a.playtime))
				FROM activity_logs a
				WHERE a.member_id = $1
					AND a.status = 'disconnected'
					AND a.playtime IS NOT NULL
					AND a.occurred_at < COALESCE((SELECT at FROM cutover), NOW())
			), 0)::bigint + COALESCE((
				-- A Discord session still open when the webhook took over ends
				-- at the handover, not at now.
				SELECT EXTRACT(EPOCH FROM (COALESCE((SELECT at FROM cutover), NOW()) - latest.started_at))
				FROM (
					SELECT status, started_at
					FROM activity_logs
					WHERE member_id = $1
					ORDER BY occurred_at DESC, id DESC
					LIMIT 1
				) latest
				WHERE latest.status = 'connected'
					AND latest.started_at IS NOT NULL
					AND latest.started_at < COALESCE((SELECT at FROM cutover), NOW())
			), 0)::bigint AS seconds
		)
		SELECT (SELECT seconds FROM server_seconds) + (SELECT seconds FROM discord_seconds),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1 AND is_attended),
			(SELECT COUNT(*) FROM attendance_logs WHERE member_id = $1)`
	if err := r.database.QueryRow(ctx, summaryQuery, memberID, visitMaxAge.Seconds()).Scan(&result.TotalPlaytimeSeconds, &result.TotalAttended, &result.TotalAttendances); err != nil {
		return MemberRecords{}, fmt.Errorf("query member records summary: %w", err)
	}

	const playerLogsQuery = `
		WITH characters AS (
			SELECT sm.id
			FROM server_members sm
			JOIN members m ON m.discord_user_id = sm.discord_user_id
			WHERE m.id = $1
		), session_starts AS (
			SELECT sl.session_id,
				MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS connected_at
			FROM server_logs sl
			WHERE sl.server_member_id IN (SELECT id FROM characters)
			GROUP BY sl.session_id
		)
		SELECT id, status, source, started_at, occurred_at, playtime_seconds FROM (
			SELECT a.id, a.status, 'discord' AS source, a.started_at, a.occurred_at,
				CASE WHEN a.playtime IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM a.playtime)::bigint END AS playtime_seconds
			FROM activity_logs a
			WHERE a.member_id = $1
			UNION ALL
			SELECT sl.id, sl.status, 'fivem', s.connected_at, sl.occurred_at,
				CASE
					WHEN sl.status = 'disconnected' AND s.connected_at IS NOT NULL
					THEN EXTRACT(EPOCH FROM (sl.occurred_at - s.connected_at))::bigint
				END
			FROM server_logs sl
			JOIN session_starts s ON s.session_id = sl.session_id
			WHERE sl.server_member_id IN (SELECT id FROM characters)
		) logs
		ORDER BY occurred_at DESC, source, id DESC`
	rows, err := r.database.Query(ctx, playerLogsQuery, memberID)
	if err != nil {
		return MemberRecords{}, fmt.Errorf("query member player logs: %w", err)
	}
	result.PlayerLogs = make([]PlayerLog, 0)
	for rows.Next() {
		var record PlayerLog
		if err := rows.Scan(&record.ID, &record.Status, &record.Source, &record.StartedAt, &record.OccurredAt, &record.PlaytimeSeconds); err != nil {
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
