package serverlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SourceCFX marks a row the bot inferred from the public Cfx.re roster.
//
// It is the third witness and the only one left when the webhook drops a visit
// and the member shows no Discord activity - invisible, or with activity
// sharing turned off. Without it that player was credited nothing at all.
const SourceCFX = "cfx"

// CFXSighting is one roster player the bot has decided opened or closed a
// visit. Username is the in-game name as CFX lists it, matched against
// server_members.username trimmed and case-insensitively, the same way the
// session sweep matches it. At is when the visit opened or last had the player
// on the list, not when the bot noticed: the roster lags, and dating the row
// to the sighting cancels most of that lag.
type CFXSighting struct {
	Username string
	ServerID int
	At       time.Time
}

// cfxCharacters resolves roster names to characters. A name shared by two
// characters goes to the one the game server saw most recently.
const cfxCharacters = `
	SELECT DISTINCT ON (LOWER(BTRIM(sm.username)))
		LOWER(BTRIM(sm.username)), sm.id, sm.player_name, sm.username, sm.cid, COALESCE(sm.discord_user_id, '')
	FROM server_members sm
	WHERE LOWER(BTRIM(sm.username)) = ANY($1::text[])
	ORDER BY LOWER(BTRIM(sm.username)), sm.updated_at DESC, sm.id DESC`

// openCFXVisits lists the visits this source has open, by roster name, so the
// bot can tell who it must see leave - including after a restart, when its
// in-memory sightings are empty but the table still has visits open.
const openCFXVisits = `
	SELECT LOWER(BTRIM(sm.username)), sl.session_id, MIN(sl.occurred_at),
		sm.id, sm.player_name, sm.username, sm.cid, COALESCE(sm.discord_user_id, '')
	FROM server_logs sl
	JOIN server_members sm ON sm.id = sl.server_member_id
	WHERE sl.source = 'cfx'
	GROUP BY sl.session_id, sm.id
	HAVING COUNT(*) FILTER (WHERE sl.status = 'disconnected') = 0`

const insertCFXLog = `
	INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at, source)
	VALUES ($1::jsonb, $2, $3, $4, $5, 'cfx')
	ON CONFLICT (payload) DO NOTHING`

// CFXOpenVisit is a visit this source has open, with the character it is on.
type CFXOpenVisit struct {
	sessionID string
	openedAt  time.Time
	character cfxCharacter
}

// OpenCFXVisits returns the open CFX visits keyed by roster name.
func (r *Repository) OpenCFXVisits(ctx context.Context) (map[string]CFXOpenVisit, error) {
	rows, err := r.pool.Query(ctx, openCFXVisits)
	if err != nil {
		return nil, fmt.Errorf("query open CFX visits: %w", err)
	}
	defer rows.Close()
	visits := make(map[string]CFXOpenVisit)
	for rows.Next() {
		var name string
		var visit CFXOpenVisit
		c := &visit.character
		if err := rows.Scan(&name, &visit.sessionID, &visit.openedAt, &c.serverMemberID, &c.playerName, &c.username, &c.cid, &c.discordUserID); err != nil {
			return nil, fmt.Errorf("scan open CFX visit: %w", err)
		}
		visits[name] = visit
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read open CFX visits: %w", err)
	}
	return visits, nil
}

// RecordCFXPresence opens a visit for every sighting in opens and closes the
// open visit of every sighting in closes, returning how many rows it wrote.
//
// The bot decides who arrived and who left, from consecutive consistent
// rosters; this only writes the rows. A name no character carries is skipped:
// the webhook is still what creates characters, so a player it has never
// reported has nobody to credit. An open that already has a visit, or a close
// with none, writes nothing, so a repeated call is harmless.
func (r *Repository) RecordCFXPresence(ctx context.Context, opens, closes []CFXSighting) (int, error) {
	if r == nil || r.pool == nil || len(opens)+len(closes) == 0 {
		return 0, nil
	}
	open, err := r.OpenCFXVisits(ctx)
	if err != nil {
		return 0, err
	}

	names := make([]string, 0, len(opens))
	for _, sighting := range opens {
		names = append(names, normalizedName(sighting.Username))
	}
	characters := make(map[string]cfxCharacter, len(names))
	if len(names) > 0 {
		rows, err := r.pool.Query(ctx, cfxCharacters, names)
		if err != nil {
			return 0, fmt.Errorf("query CFX characters: %w", err)
		}
		for rows.Next() {
			var name string
			var character cfxCharacter
			if err := rows.Scan(&name, &character.serverMemberID, &character.playerName, &character.username, &character.cid, &character.discordUserID); err != nil {
				rows.Close()
				return 0, fmt.Errorf("scan CFX character: %w", err)
			}
			characters[name] = character
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read CFX characters: %w", err)
		}
		rows.Close()
	}

	written := 0
	for _, sighting := range opens {
		name := normalizedName(sighting.Username)
		character, known := characters[name]
		if !known {
			continue
		}
		if _, already := open[name]; already {
			continue
		}
		session, err := newSessionID()
		if err != nil {
			return written, err
		}
		wrote, err := r.insertCFXLog(ctx, character, session, StatusConnected, sighting)
		if err != nil {
			return written, err
		}
		if wrote {
			written++
		}
	}

	for _, sighting := range closes {
		visit, found := open[normalizedName(sighting.Username)]
		if !found {
			continue
		}
		// A visit cannot end before it began, whatever the bot remembered.
		if sighting.At.Before(visit.openedAt) {
			sighting.At = visit.openedAt
		}
		wrote, err := r.insertCFXLog(ctx, visit.character, visit.sessionID, StatusDisconnected, sighting)
		if err != nil {
			return written, err
		}
		if wrote {
			written++
		}
	}
	return written, nil
}

type cfxCharacter struct {
	serverMemberID int64
	playerName     string
	username       string
	cid            string
	discordUserID  string
}

func (r *Repository) insertCFXLog(ctx context.Context, character cfxCharacter, session, status string, sighting CFXSighting) (bool, error) {
	payload, err := cfxPayload(character, status, sighting)
	if err != nil {
		return false, err
	}
	// ON CONFLICT DO NOTHING affects no row for a payload already stored,
	// which is a replay, not a failure.
	tag, err := r.pool.Exec(ctx, insertCFXLog, payload, character.serverMemberID, session, status, sighting.At.UTC())
	if err != nil {
		return false, fmt.Errorf("insert CFX server log: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// cfxPayload shapes the row like a webhook event, as discordPayload does, so
// every reader keeps working without knowing a third writer exists. The CFX
// player id is the slot, which the announcement and the dashboard show.
func cfxPayload(character cfxCharacter, status string, sighting CFXSighting) ([]byte, error) {
	player := map[string]any{
		"name":     character.playerName,
		"username": character.username,
		"cid":      character.cid,
	}
	if sighting.ServerID > 0 {
		player["server_id"] = sighting.ServerID
	}
	if character.discordUserID != "" {
		player["identifiers"] = map[string]any{"discord": character.discordUserID}
	}
	payload, err := json.Marshal(map[string]any{
		"player": player,
		"event": map[string]any{
			"type":      status,
			"timestamp": sighting.At.UTC().Format(time.RFC3339Nano),
		},
		"source": SourceCFX,
	})
	if err != nil {
		return nil, fmt.Errorf("encode CFX server log payload: %w", err)
	}
	return payload, nil
}

func normalizedName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
