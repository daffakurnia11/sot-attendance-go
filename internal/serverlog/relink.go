package serverlog

import (
	"context"
	"fmt"
)

// relinkAll attaches unmatched server identities to registered members.
//
// The contract promises that an event accepted with matched_member false is
// linked later without a resend. This is that promise: a player who connects to
// FiveM before registering on Discord, which is the common order, is picked up
// once the member row exists.
const relinkAll = `
	UPDATE server_members sm
	SET member_id = m.id, updated_at = NOW()
	FROM members m
	WHERE sm.member_id IS NULL
		AND sm.discord_user_id IS NOT NULL
		AND m.user_id = sm.discord_user_id`

// Relink links every unmatched identity it can and reports how many rows moved.
//
// This sweep is the only linking mechanism. Members are created in bulk by the
// Discord guild sync rather than at one registration call site, so there is no
// single place to link a user the moment they register: a newly registered
// member is picked up on the next sweep instead. Safe to run on a schedule; a
// run that links nothing is the normal case.
func (r *Repository) Relink(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, relinkAll)
	if err != nil {
		return 0, fmt.Errorf("relink server members: %w", err)
	}
	return tag.RowsAffected(), nil
}
