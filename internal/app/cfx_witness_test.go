package app

import (
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

func TestCFXWitnessOpensAfterTheArrivalStreak(t *testing.T) {
	t.Parallel()
	var witness cfxWitness
	start := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	roster := []dashboard.CFXPlayer{{ID: 763, Name: " sot x ywd - MICI "}}

	if opens, _ := witness.observe(roster, nil, start); len(opens) != 0 {
		t.Fatalf("first sighting opened %#v, want nothing until the streak", opens)
	}
	opens, closes := witness.observe(roster, nil, start.Add(time.Minute))
	if len(opens) != 1 || len(closes) != 0 {
		t.Fatalf("second sighting = %#v / %#v, want one open", opens, closes)
	}
	if opens[0].At != start || opens[0].ServerID != 763 || opens[0].Username != "sot x ywd - MICI" {
		t.Errorf("open = %#v, want the first sighting, slot 763, trimmed name", opens[0])
	}
}

func TestCFXWitnessArrivalStreakIsConsecutive(t *testing.T) {
	t.Parallel()
	var witness cfxWitness
	start := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	roster := []dashboard.CFXPlayer{{ID: 1, Name: "SOT - Paw"}}

	witness.observe(roster, nil, start)
	witness.observe(nil, nil, start.Add(time.Minute))
	if opens, _ := witness.observe(roster, nil, start.Add(2*time.Minute)); len(opens) != 0 {
		t.Fatalf("a gap between sightings still opened %#v", opens)
	}
}

func TestCFXWitnessClosesAtTheLastSighting(t *testing.T) {
	t.Parallel()
	var witness cfxWitness
	start := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	roster := []dashboard.CFXPlayer{{ID: 1, Name: "SOT - Paw"}}
	open := map[string]serverlog.CFXOpenVisit{"sot - paw": {}}

	witness.observe(roster, open, start)
	lastSeen := start.Add(time.Minute)
	witness.observe(roster, open, lastSeen)
	for minute := 2; minute < 1+cfxDepartureStreak; minute++ {
		if _, closes := witness.observe(nil, open, start.Add(time.Duration(minute)*time.Minute)); len(closes) != 0 {
			t.Fatalf("closed after %d absent reads, want %d", minute-1, cfxDepartureStreak)
		}
	}
	_, closes := witness.observe(nil, open, start.Add(time.Duration(1+cfxDepartureStreak)*time.Minute))
	if len(closes) != 1 || !closes[0].At.Equal(lastSeen) {
		t.Fatalf("closes = %#v, want one dated to the last sighting %s", closes, lastSeen)
	}
}

func TestCFXWitnessAbsenceResetsWhenListedAgain(t *testing.T) {
	t.Parallel()
	var witness cfxWitness
	start := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	roster := []dashboard.CFXPlayer{{ID: 1, Name: "SOT - Paw"}}
	open := map[string]serverlog.CFXOpenVisit{"sot - paw": {}}

	witness.observe(roster, open, start)
	witness.observe(nil, open, start.Add(time.Minute))
	witness.observe(nil, open, start.Add(2*time.Minute))
	witness.observe(roster, open, start.Add(3*time.Minute))
	for minute := 4; minute < 4+cfxDepartureStreak-1; minute++ {
		if _, closes := witness.observe(nil, open, start.Add(time.Duration(minute)*time.Minute)); len(closes) != 0 {
			t.Fatalf("closed at minute %d: absence should have reset when the player was listed again", minute)
		}
	}
}

// After a restart the bot has no sightings, but the table still has visits
// open. They must still close, dated to when the absence was first noticed.
func TestCFXWitnessClosesVisitsLeftOpenAcrossARestart(t *testing.T) {
	t.Parallel()
	var witness cfxWitness
	start := time.Date(2026, 9, 24, 20, 0, 0, 0, time.UTC)
	open := map[string]serverlog.CFXOpenVisit{"sot - paw": {}}

	var closes []serverlog.CFXSighting
	for read := 0; read < cfxDepartureStreak; read++ {
		_, got := witness.observe(nil, open, start.Add(time.Duration(read)*time.Minute))
		closes = append(closes, got...)
	}
	if len(closes) != 1 || !closes[0].At.Equal(start) {
		t.Fatalf("closes = %#v, want one dated to the first absent read", closes)
	}
}
