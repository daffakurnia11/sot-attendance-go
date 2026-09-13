package app

import "testing"

// A single read that does not see the activity must not close a visit. FiveM
// rewrites the activity as a player moves from joining to playing, and a poll
// landing inside that rewrite used to read as "stopped playing" and close the
// visit one tick after opening it.
func TestDiscordAbsenceStreakDelaysClosure(t *testing.T) {
	t.Parallel()
	bot := &Bot{discordAbsence: make(map[string]int)}

	for poll := 1; poll < discordAbsenceStreak; poll++ {
		if bot.observeDiscordActivity("ken", false) {
			t.Fatalf("visit closed after %d absent polls, want at least %d", poll, discordAbsenceStreak)
		}
	}
	if !bot.observeDiscordActivity("ken", false) {
		t.Fatalf("visit still open after %d absent polls", discordAbsenceStreak)
	}
}

// Seeing the activity always acts, and clears the count, so a flicker mid-visit
// does not accumulate towards a closure hours later.
func TestDiscordAbsenceResetsWhenSeenAgain(t *testing.T) {
	t.Parallel()
	bot := &Bot{discordAbsence: make(map[string]int)}

	bot.observeDiscordActivity("ken", false)
	bot.observeDiscordActivity("ken", false)
	if !bot.observeDiscordActivity("ken", true) {
		t.Fatal("a member seen playing was skipped")
	}
	if count := bot.discordAbsence["ken"]; count != 0 {
		t.Fatalf("absence count after a sighting = %d, want 0", count)
	}
	for poll := 1; poll < discordAbsenceStreak; poll++ {
		if bot.observeDiscordActivity("ken", false) {
			t.Fatalf("visit closed after %d absent polls following a sighting", poll)
		}
	}
}

// One member's absence must not count towards another's.
func TestDiscordAbsenceIsPerMember(t *testing.T) {
	t.Parallel()
	bot := &Bot{discordAbsence: make(map[string]int)}
	for poll := 0; poll < discordAbsenceStreak; poll++ {
		bot.observeDiscordActivity("ken", false)
	}
	if bot.observeDiscordActivity("ayvix", false) {
		t.Fatal("a second member closed on their first absent poll")
	}
}

// A snapshot smaller than the previous one is a gateway cache still filling
// after a reconnect, not a guild that emptied: an offline member keeps a
// presence entry, so the count only falls when entries have not arrived yet.
// Closing on that reading ended live visits one poll after a restart.
func TestDiscordVisitsIgnoreAShrinkingSnapshot(t *testing.T) {
	t.Parallel()
	bot := &Bot{discordAbsence: make(map[string]int), discordObserved: 21}

	// Five presences where twenty-one were seen: nobody may be closed.
	if !bot.presenceCacheFilling(5) {
		t.Fatal("a snapshot of 5 after 21 was not treated as still filling")
	}
	// Once the count stops falling, closures resume on the usual streak.
	if bot.presenceCacheFilling(5) {
		t.Fatal("a steady snapshot was still treated as filling")
	}
	// A growing snapshot is a filled cache, not a filling one.
	if bot.presenceCacheFilling(21) {
		t.Fatal("a growing snapshot was treated as filling")
	}
	for poll := 1; poll < discordAbsenceStreak; poll++ {
		if bot.observeDiscordActivity("mepi", false) {
			t.Fatalf("visit closed after %d absent polls", poll)
		}
	}
	if !bot.observeDiscordActivity("mepi", false) {
		t.Fatal("visit never closed once the snapshot was steady")
	}
}
