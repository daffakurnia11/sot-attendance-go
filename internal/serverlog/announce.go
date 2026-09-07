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
	// ServerID is the slot the game server assigned, and Reason the disconnect
	// reason. Both live in payload only, and both are absent on events that
	// carry no such value, so both are empty strings rather than pointers: the
	// renderer omits what is empty.
	ServerID string
	Reason   string
}

const latestEventID = `SELECT COALESCE(MAX(id), 0) FROM server_logs`

const announcementsAfter = `
	SELECT sl.id,
	       COALESCE(sl.payload->'player'->>'name', sm.player_name),
	       COALESCE(sl.payload->'player'->>'username', sm.username),
	       sl.status,
	       sl.occurred_at,
	       sl.payload->'player'->>'server_id',
	       sl.payload->'event'->>'reason'
	FROM server_logs sl
	JOIN server_members sm ON sm.id = sl.server_member_id
	WHERE sl.id > $1
	ORDER BY sl.id
	LIMIT $2`

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
		var (
			a        Announcement
			serverID *string
			reason   *string
		)
		if err := rows.Scan(&a.ID, &a.PlayerName, &a.Username, &a.Status, &a.OccurredAt, &serverID, &reason); err != nil {
			return nil, fmt.Errorf("scan server log announcement: %w", err)
		}
		if serverID != nil {
			a.ServerID = *serverID
		}
		if reason != nil {
			a.Reason = *reason
		}
		announcements = append(announcements, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read server log announcements: %w", err)
	}
	return announcements, nil
}
