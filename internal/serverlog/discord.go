package serverlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// SourceServer marks a row the CR Roleplay webhook reported; SourceDiscord a
// row the bot inferred from a member's Discord activity.
const (
	SourceServer  = "server"
	SourceDiscord = "discord"
)

// DiscordPresence is one member's observed activity for the server, as the
// gateway reports it.
type DiscordPresence struct {
	DiscordUserID string
	Playing       bool
	// StartedAt is when the Discord activity began, used as the visit start so
	// a poll that first sees a member mid-session does not date the visit to
	// the tick that noticed it. Zero when Discord supplied no timestamp.
	StartedAt time.Time
}

// discordCharacters lists the characters a Discord account has on
// server_members, newest visit first, so a member with several characters has
// their activity attributed to the one the game server saw most recently.
const discordCharacters = `
	SELECT DISTINCT ON (sm.discord_user_id)
		sm.discord_user_id, sm.id, sm.player_name, sm.username, sm.cid
	FROM server_members sm
	WHERE sm.discord_user_id = ANY($1::text[])
	ORDER BY sm.discord_user_id, sm.updated_at DESC, sm.id DESC`

// openDiscordVisit finds the visit this source already has open for a
// character, so a disconnect closes the session its connect opened.
//
// Scoped to source = 'discord' on purpose: the webhook owns its own sessions
// and a poll must never close one of them. When both sources see the same
// visit, each keeps its own pair of rows and the reader decides which to trust.
const openDiscordVisit = `
	SELECT sl.session_id, sl.occurred_at
	FROM server_logs sl
	WHERE sl.source = 'discord'
		AND sl.server_member_id = $1
		AND sl.status IN ('connecting', 'connected')
		AND NOT EXISTS (
			SELECT 1 FROM server_logs closed
			WHERE closed.source = 'discord'
				AND closed.session_id = sl.session_id
				AND closed.status = 'disconnected'
		)
	ORDER BY sl.occurred_at DESC, sl.id DESC
	LIMIT 1`

const insertDiscordLog = `
	INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
	VALUES ($1::jsonb, $2, $3, $4, $5, 'discord')
	ON CONFLICT (payload) DO NOTHING
	RETURNING id`

type discordCharacter struct {
	serverMemberID int64
	playerName     string
	username       string
	cid            string
}

// RecordDiscordPresence writes a row for every character whose Discord activity
// disagrees with the visit this source has open for them, and returns how many
// it wrote.
//
// Only transitions are stored, so a quiet poll writes nothing and the table
// grows with activity rather than with uptime. A member Discord has nothing to
// say about is absent from the snapshot and is left alone: an unseen member is
// not an absent one, and closing their visit on silence would end a visit the
// webhook may still be reporting.
//
// ponytail: one row per transition per tick, written in a loop rather than a
// single multi-row statement. Transitions are rare next to the roster size;
// batch the insert if a poll ever writes enough rows to matter.
func (r *Repository) RecordDiscordPresence(ctx context.Context, observed []DiscordPresence, serverKey string) (int, error) {
	if r == nil || r.pool == nil || len(observed) == 0 {
		return 0, nil
	}
	userIDs := make([]string, 0, len(observed))
	for _, entry := range observed {
		if entry.DiscordUserID != "" {
			userIDs = append(userIDs, entry.DiscordUserID)
		}
	}
	if len(userIDs) == 0 {
		return 0, nil
	}

	rows, err := r.pool.Query(ctx, discordCharacters, userIDs)
	if err != nil {
		return 0, fmt.Errorf("query Discord characters: %w", err)
	}
	characters := make(map[string]discordCharacter, len(userIDs))
	for rows.Next() {
		var discordUserID string
		var character discordCharacter
		if err := rows.Scan(&discordUserID, &character.serverMemberID, &character.playerName, &character.username, &character.cid); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan Discord character: %w", err)
		}
		characters[discordUserID] = character
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read Discord characters: %w", err)
	}
	rows.Close()

	written := 0
	for _, entry := range observed {
		character, known := characters[entry.DiscordUserID]
		// A member the game server has never reported has no character to
		// attribute the activity to, so there is nothing to write.
		if !known {
			continue
		}
		var openSession *string
		var openedAt time.Time
		var sessionID string
		err := r.pool.QueryRow(ctx, openDiscordVisit, character.serverMemberID).Scan(&sessionID, &openedAt)
		switch {
		case err == nil:
			openSession = &sessionID
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return written, fmt.Errorf("read open Discord visit: %w", err)
		}

		status, session, occurredAt, write := discordTransition(entry, openSession)
		if !write {
			continue
		}
		if session == "" {
			session, err = newSessionID()
			if err != nil {
				return written, err
			}
		}
		payload, err := discordPayload(character, entry, status, occurredAt, serverKey)
		if err != nil {
			return written, err
		}
		var id int64
		if err := r.pool.QueryRow(ctx, insertDiscordLog, payload, character.serverMemberID, session, status, occurredAt).Scan(&id); err != nil {
			// ON CONFLICT DO NOTHING returns no row when an identical
			// payload is already stored, which is a duplicate, not a failure.
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return written, fmt.Errorf("insert Discord server log: %w", err)
		}
		written++
	}
	return written, nil
}

// discordTransition decides what a single observation should write.
//
// Playing with no visit open opens one; not playing with a visit open closes
// it. Everything else is the state the table already holds, so nothing is
// written and the poll stays silent.
func discordTransition(entry DiscordPresence, openSession *string) (status, session string, occurredAt time.Time, write bool) {
	now := time.Now().UTC()
	switch {
	case entry.Playing && openSession == nil:
		occurredAt = now
		// Dating the visit to when Discord says the activity began, not to the
		// tick that noticed it, so a poller started mid-session credits the
		// whole visit rather than the part it watched.
		if !entry.StartedAt.IsZero() && entry.StartedAt.Before(now) {
			occurredAt = entry.StartedAt.UTC()
		}
		return StatusConnected, "", occurredAt, true
	case !entry.Playing && openSession != nil:
		return StatusDisconnected, *openSession, now, true
	default:
		return "", "", time.Time{}, false
	}
}

// discordPayload shapes the row like a webhook event, so every reader of
// server_logs - the announcer, the dashboard, the recap - keeps working without
// knowing a second writer exists. The source column is what tells them apart.
func discordPayload(character discordCharacter, entry DiscordPresence, status string, occurredAt time.Time, serverKey string) ([]byte, error) {
	event := map[string]any{
		"player": map[string]any{
			"name":     character.playerName,
			"username": character.username,
			"cid":      character.cid,
			"identifiers": map[string]any{
				"discord": entry.DiscordUserID,
			},
		},
		"event": map[string]any{
			"type":      status,
			"timestamp": occurredAt.Format(time.RFC3339Nano),
			"server":    serverKey,
		},
		// Named in the payload as well as the column so a row read straight out
		// of the table is self-describing.
		"source": SourceDiscord,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode Discord server log payload: %w", err)
	}
	return payload, nil
}
