package app

import (
	"context"
	"strings"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

// cfxArrivalStreak is how many consecutive consistent rosters must list a
// player before the bot records their visit. One sighting is enough to be sure
// they were on the list, but the list is a cache: two in a row rules out a
// stale entry for someone already gone.
const cfxArrivalStreak = 2

// cfxDepartureStreak is how many consecutive consistent rosters must omit a
// player before the bot closes their visit. With the default one-minute poll a
// CFX-only exit is recorded about three minutes late, but it is dated to the
// last sighting, so playtime loses none of that.
const cfxDepartureStreak = 3

// cfxWitness turns a series of roster reads into visit openings and closings.
//
// It holds the per-name streaks in memory and nothing else: the table is the
// record, and a restart costs only the streaks, which rebuild within a few
// polls. A visit left open across a restart is still closed, because the open
// visits are read back from the table on every poll.
type cfxWitness struct {
	sightings map[string]*cfxSightingState
}

type cfxSightingState struct {
	username  string
	serverID  int
	firstSeen time.Time
	lastSeen  time.Time
	seen      int
	absent    int
	// absentSince dates a close when the player was never seen by this
	// process - a visit left open across a restart - so the visit ends when
	// its absence was first noticed rather than whenever the streak is met.
	absentSince time.Time
}

// observe records one consistent roster and returns the visits to open and
// close. open is every roster name that has a CFX visit open in the table.
//
// Only a roster whose list matches the count the server reports for itself
// may be passed in: a truncated read looks exactly like players leaving, and
// the caller skips those before they get here.
func (w *cfxWitness) observe(roster []dashboard.CFXPlayer, open map[string]serverlog.CFXOpenVisit, now time.Time) (opens, closes []serverlog.CFXSighting) {
	if w.sightings == nil {
		w.sightings = make(map[string]*cfxSightingState)
	}
	listed := make(map[string]bool, len(roster))
	for _, player := range roster {
		name := strings.ToLower(strings.TrimSpace(player.Name))
		if name == "" || listed[name] {
			continue
		}
		listed[name] = true
		state := w.sightings[name]
		if state == nil {
			state = &cfxSightingState{username: strings.TrimSpace(player.Name), firstSeen: now}
			w.sightings[name] = state
		}
		state.serverID = player.ID
		state.lastSeen = now
		state.seen++
		state.absent = 0
		state.absentSince = time.Time{}
		if _, isOpen := open[name]; state.seen >= cfxArrivalStreak && !isOpen {
			opens = append(opens, serverlog.CFXSighting{Username: state.username, ServerID: state.serverID, At: state.firstSeen})
		}
	}

	for name := range open {
		if listed[name] {
			continue
		}
		state := w.sightings[name]
		if state == nil {
			state = &cfxSightingState{username: name}
			w.sightings[name] = state
		}
		if state.absent == 0 {
			state.absentSince = now
		}
		state.absent++
		if state.absent < cfxDepartureStreak {
			continue
		}
		endedAt := state.lastSeen
		if endedAt.IsZero() {
			endedAt = state.absentSince
		}
		closes = append(closes, serverlog.CFXSighting{Username: state.username, ServerID: state.serverID, At: endedAt})
		delete(w.sightings, name)
	}

	// A name neither listed nor open has nothing left to track; dropping it
	// is what makes the arrival streak consecutive.
	for name := range w.sightings {
		if _, isOpen := open[name]; !listed[name] && !isOpen {
			delete(w.sightings, name)
		}
	}
	return opens, closes
}

// recordCFXVisits writes the CFX witness's view of one roster read.
//
// Called only with a roster the caller has checked is consistent, from the
// same goroutine as every other CFX read, so the streaks need no lock.
func (b *Bot) recordCFXVisits(ctx context.Context, roster []dashboard.CFXPlayer) {
	if !b.announces || b.serverLogs == nil {
		return
	}
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	open, err := b.serverLogs.OpenCFXVisits(requestContext)
	if err != nil {
		b.logger.Error("read open CFX visits", "error", err)
		return
	}
	opens, closes := b.cfxVisits.observe(roster, open, time.Now().UTC())
	written, err := b.serverLogs.RecordCFXPresence(requestContext, opens, closes)
	if err != nil {
		b.logger.Error("record CFX visits", "error", err)
		return
	}
	if written > 0 {
		b.logger.Info("CFX visits recorded", "transitions", written, "listed", len(roster))
	}
}
