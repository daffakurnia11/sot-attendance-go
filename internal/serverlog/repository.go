package serverlog

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// upsertMember writes the identity and resolves the SOT member in one
// statement.
//
// Identity is (cid, steamhex): the character plus the Steam account it played
// from. Neither discord_user_id nor license_id is stable enough to key on - one
// person legitimately holds several rows under one Discord id, and a license
// changes on reinstall - so both are treated as mutable data instead.
//
// server_members carries no foreign key to members. It records who the game
// server saw, registered or not; member_id is a soft link resolved from the
// Discord id when one matches.
//
// The previous CTE reads the pre-insert row, because a CTE sees the snapshot
// taken when the statement started. That gives the caller the identifier
// disagreements without a second round trip.
//
// member_id is wrapped in COALESCE so an existing link is never replaced, and
// the optional identifiers are too so a missing value never clears a stored
// one. Profile fields overwrite unconditionally: a retry can briefly write a
// name a few seconds stale and the next event corrects it.
const upsertMember = `
	WITH previous AS (
		SELECT license_id, discord_user_id, fivem_id
		FROM server_members
		WHERE cid = $7 AND steamhex = $4
	), upserted AS (
		INSERT INTO server_members (license_id, member_id, discord_user_id, fivem_id, steamhex,
		                            player_name, username, cid)
		VALUES ($1, (SELECT m.id FROM members m WHERE m.user_id = $2), $2, $3, $4, $5, $6, $7)
		ON CONFLICT (cid, steamhex) DO UPDATE SET
			license_id      = EXCLUDED.license_id,
			discord_user_id = COALESCE(EXCLUDED.discord_user_id, server_members.discord_user_id),
			fivem_id        = COALESCE(EXCLUDED.fivem_id, server_members.fivem_id),
			player_name     = EXCLUDED.player_name,
			username        = EXCLUDED.username,
			member_id       = COALESCE(
				server_members.member_id,
				(SELECT m.id FROM members m
				 WHERE m.user_id = COALESCE(EXCLUDED.discord_user_id, server_members.discord_user_id))
			),
			updated_at      = NOW()
		RETURNING id, member_id
	)
	SELECT u.id,
	       u.member_id,
	       COALESCE(p.discord_user_id IS NOT NULL AND $2 IS NOT NULL AND p.discord_user_id <> $2, false),
	       COALESCE(p.fivem_id        IS NOT NULL AND $3 IS NOT NULL AND p.fivem_id        <> $3, false),
	       COALESCE(p.license_id      <> $1, false)
	FROM upserted u
	LEFT JOIN previous p ON true`

// lockEvent serialises concurrent deliveries of the same body so the duplicate
// check below cannot race. The key is a hash of the body text, computed by
// Postgres and never stored.
const lockEvent = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

const findEvent = `
	SELECT sl.session_id, sl.server_member_id, sm.member_id
	FROM server_logs sl
	JOIN server_members sm ON sm.id = sl.server_member_id
	WHERE sl.payload = $1::jsonb`

// findOpenSession is how a session is reconstructed without the sender tracking
// one. A session is open until a disconnected event lands for it, so the most
// recent open session for this player is the one a connected or disconnected
// event belongs to.
//
// Cost of deriving rather than being told: a disconnected event that overtakes
// its own connecting event finds no open session and starts a new one. That
// splits one visit into two rows instead of losing it, which is the better
// failure of the two.
const findOpenSession = `
	SELECT sl.session_id
	FROM server_logs sl
	WHERE sl.server_member_id = $1
		AND NOT EXISTS (
			SELECT 1 FROM server_logs d
			WHERE d.session_id = sl.session_id AND d.status = 'disconnected'
		)
	ORDER BY sl.occurred_at DESC, sl.id DESC
	LIMIT 1`

const insertLog = `
	INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
	VALUES ($1::jsonb, $2, $3, $4, $5)
	ON CONFLICT (payload) DO NOTHING
	RETURNING id`

// newSessionID returns a random UUID v4. The sender no longer supplies one, so
// the backend mints it when a session opens.
func newSessionID() (string, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16]), nil
}

// Store writes one event. It is safe to call concurrently and safe to call
// again with the same event_id.
func (r *Repository) Store(ctx context.Context, event ValidEvent) (AcceptedResult, error) {
	transaction, err := r.pool.Begin(ctx)
	if err != nil {
		return AcceptedResult{}, fmt.Errorf("begin server log transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	// Serialize equal event IDs, then check before touching identity data. This
	// makes a retry immutable even when two copies arrive concurrently.
	if _, err := transaction.Exec(ctx, lockEvent, string(event.Payload)); err != nil {
		return AcceptedResult{}, fmt.Errorf("lock server log event: %w", err)
	}
	var (
		existingSessionID      string
		existingServerMemberID int64
		existingMemberID       *int64
	)
	err = transaction.QueryRow(ctx, findEvent, string(event.Payload)).Scan(&existingSessionID, &existingServerMemberID, &existingMemberID)
	switch {
	case err == nil:
		if err := transaction.Commit(ctx); err != nil {
			return AcceptedResult{}, fmt.Errorf("commit duplicate server log transaction: %w", err)
		}
		return AcceptedResult{
			SessionID:      existingSessionID,
			Duplicate:      true,
			MatchedMember:  existingMemberID != nil,
			ServerMemberID: existingServerMemberID,
			MemberID:       existingMemberID,
		}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return AcceptedResult{}, fmt.Errorf("find server log event: %w", err)
	}

	var (
		serverMemberID                               int64
		memberID                                     *int64
		discordDiffers, fivemDiffers, licenseDiffers bool
	)
	err = transaction.QueryRow(ctx, upsertMember,
		event.License, event.Discord, event.FiveM, event.SteamHex,
		event.PlayerName, event.Username, event.CID,
	).Scan(&serverMemberID, &memberID, &discordDiffers, &fivemDiffers, &licenseDiffers)
	if err != nil {
		return AcceptedResult{}, fmt.Errorf("upsert server member: %w", err)
	}

	sessionID, err := resolveSession(ctx, transaction, event, serverMemberID)
	if err != nil {
		return AcceptedResult{}, err
	}

	duplicate := false
	var logID int64
	err = transaction.QueryRow(ctx, insertLog,
		string(event.Payload), serverMemberID, sessionID, event.Status, event.OccurredAt,
	).Scan(&logID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Defence in depth. The advisory lock and findEvent above should have
		// caught this, but ON CONFLICT DO NOTHING returning no rows still means
		// the event is already stored. That is a success, and answering 500
		// here would make the sender retry a delivery that can never succeed.
		duplicate = true
	case err != nil:
		return AcceptedResult{}, fmt.Errorf("insert server log: %w", err)
	}

	if err := transaction.Commit(ctx); err != nil {
		return AcceptedResult{}, fmt.Errorf("commit server log transaction: %w", err)
	}

	var mismatched []string
	if discordDiffers {
		mismatched = append(mismatched, "discord")
	}
	if fivemDiffers {
		mismatched = append(mismatched, "fivem")
	}
	if licenseDiffers {
		// Same character on the same Steam account, reporting a different
		// Rockstar license. Legitimate after a reinstall, worth a look if not.
		mismatched = append(mismatched, "license")
	}

	return AcceptedResult{
		SessionID:        sessionID,
		Duplicate:        duplicate,
		MatchedMember:    memberID != nil,
		ServerMemberID:   serverMemberID,
		MemberID:         memberID,
		IdentityMismatch: mismatched,
	}, nil
}

// resolveSession decides which session this event belongs to.
//
// A connecting event always opens a new one: it marks the start of an attempt,
// and a retry of it never reaches here because the body hash already deduped.
// The other two statuses attach to the player's open session, and open one
// themselves when none exists so a missed connecting event costs nothing.
func resolveSession(ctx context.Context, transaction pgx.Tx, event ValidEvent, serverMemberID int64) (string, error) {
	if event.Status != StatusConnecting {
		var sessionID string
		err := transaction.QueryRow(ctx, findOpenSession, serverMemberID).Scan(&sessionID)
		switch {
		case err == nil:
			return sessionID, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return "", fmt.Errorf("find open server session: %w", err)
		}
	}
	return newSessionID()
}
