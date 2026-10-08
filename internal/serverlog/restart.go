package serverlog

import (
	"context"
	"fmt"
	"time"
)

// CloseForRestart records an exit at the restart boundary for every player
// still connected: the newest event of the character on that source, after the
// previous restart, is connecting or connected. Older open visits are not
// players still online but sessions whose disconnect never arrived; the
// previous restart, or a later visit, already ended them. Keeping the original
// source and session lets all existing readers and the Discord announcer
// consume the transition normally.
func (r *Repository) CloseForRestart(ctx context.Context, at, previous time.Time) (int64, error) {
	result, err := r.pool.Exec(ctx, `
 INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
 SELECT jsonb_build_object(
  'event', jsonb_build_object('type', 'disconnected', 'reason', 'Scheduled restart', 'timestamp', $1::timestamptz),
  'player', jsonb_build_object('name', sm.player_name, 'username', sm.username, 'cid', sm.cid, 'session_id', sl.session_id::text)
 ), sl.server_member_id, sl.session_id, 'disconnected', $1, sl.source
 FROM (SELECT DISTINCT ON (server_member_id, source) * FROM server_logs
  WHERE occurred_at <= $1 AND occurred_at > $2
  ORDER BY server_member_id, source, occurred_at DESC, id DESC) sl
 JOIN server_members sm ON sm.id = sl.server_member_id
 WHERE sl.status IN ('connecting', 'connected')
  AND NOT EXISTS (SELECT 1 FROM server_logs closed
  WHERE closed.session_id = sl.session_id AND closed.source = sl.source AND closed.status = 'disconnected')
 ON CONFLICT (payload) DO NOTHING`, at, previous)
	if err != nil {
		return 0, fmt.Errorf("close visits for scheduled restart: %w", err)
	}
	return result.RowsAffected(), nil
}
