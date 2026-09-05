package serverlog

import (
	"context"
	"fmt"
	"time"
)

// Announcement is one stored event, flattened for the Discord player log.
//
// PlayerName and Username come out of the stored payload rather than a column:
// server_logs keeps only the six fields the feature needs, and the rest of the
// event survives inside payload. Rows written before payload existed fall back
// to the latest values on server_members.
type Announcement struct {
	ID         int64
	PlayerName string
	Username   string
	Status     string
	OccurredAt time.Time
	// StartedAt is the first event time of the visit, so a disconnect can show
	// how long the player was on. Equal to OccurredAt for the opening event.
	StartedAt time.Time
}

const latestEventID = `SELECT COALESCE(MAX(id), 0) FROM server_logs`

const announcementsAfter = `
	SELECT sl.id,
	       COALESCE(sl.payload->'player'->>'name', sm.player_name),
	       COALESCE(sl.payload->'player'->>'username', sm.username),
	       sl.status,
	       sl.occurred_at,
	       (SELECT MIN(s2.occurred_at) FROM server_logs s2 WHERE s2.session_id = sl.session_id)
	FROM server_logs sl
	JOIN server_members sm ON sm.id = sl.server_member_id
	WHERE sl.id > $1
	ORDER BY sl.id
	LIMIT $2`

// connectedPlayers counts players whose latest event was connected.
//
// It replaced a count of visits with no disconnected event, which overcounted:
// a visit whose disconnected event never arrived stayed open forever and kept
// being counted. This is deliberately not the Discord presence count - the two
// disagreeing is the signal this feature exists to surface, so each log source
// shows its own tally.
const connectedPlayers = `
	SELECT count(*) FROM server_members WHERE last_status = 'connected'`

// LatestEventID is the cursor seed. Starting from the newest row means a first
// run announces nothing rather than replaying the whole table into the channel.
func (r *Repository) LatestEventID(ctx context.Context) (int64, error) {
	var id int64
	if err := r.pool.QueryRow(ctx, latestEventID).Scan(&id); err != nil {
		return 0, fmt.Errorf("latest server log id: %w", err)
	}
	return id, nil
}

// AnnouncementsAfter returns events newer than afterID, oldest first, so the
// caller can post them in order and advance its cursor to the last ID it saw.
func (r *Repository) AnnouncementsAfter(ctx context.Context, afterID int64, limit int) ([]Announcement, error) {
	rows, err := r.pool.Query(ctx, announcementsAfter, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("query server log announcements: %w", err)
	}
	defer rows.Close()

	var announcements []Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.PlayerName, &a.Username, &a.Status, &a.OccurredAt, &a.StartedAt); err != nil {
			return nil, fmt.Errorf("scan server log announcement: %w", err)
		}
		announcements = append(announcements, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read server log announcements: %w", err)
	}
	return announcements, nil
}

// ConnectedPlayerCount reports how many players the FiveM server currently has
// on, meaning those whose latest event was connected.
func (r *Repository) ConnectedPlayerCount(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, connectedPlayers).Scan(&count); err != nil {
		return 0, fmt.Errorf("count connected server players: %w", err)
	}
	return count, nil
}
