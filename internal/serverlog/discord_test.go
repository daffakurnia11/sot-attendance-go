package serverlog

import (
	"strings"
	"testing"
	"time"
)

func session(id string) *string { return &id }

// Only a disagreement between Discord and the visit this source has open is
// written. A poll where nothing changed must write nothing, or the table grows
// with uptime instead of with activity.
func TestDiscordTransitionWritesOnlyChanges(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		entry      DiscordPresence
		open       *string
		wantWrite  bool
		wantStatus string
	}{
		{name: "playing with no visit opens one", entry: DiscordPresence{Playing: true}, wantWrite: true, wantStatus: StatusConnected},
		{name: "stopped playing closes the visit", entry: DiscordPresence{}, open: session("s1"), wantWrite: true, wantStatus: StatusDisconnected},
		{name: "still playing writes nothing", entry: DiscordPresence{Playing: true}, open: session("s1")},
		{name: "still not playing writes nothing", entry: DiscordPresence{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			status, _, _, write := discordTransition(test.entry, test.open)
			if write != test.wantWrite || status != test.wantStatus {
				t.Fatalf("discordTransition() = %q, %v; want %q, %v", status, write, test.wantStatus, test.wantWrite)
			}
		})
	}
}

// A close reuses the session its connect opened, so the pair reads as one visit
// the same way a webhook pair does.
func TestDiscordTransitionClosesTheOpenSession(t *testing.T) {
	t.Parallel()
	_, got, _, _ := discordTransition(DiscordPresence{}, session("visit-1"))
	if got != "visit-1" {
		t.Fatalf("session = %q, want visit-1", got)
	}
	// An opening event has no session yet; the caller mints one.
	if _, got, _, _ := discordTransition(DiscordPresence{Playing: true}, nil); got != "" {
		t.Fatalf("session on open = %q, want empty", got)
	}
}

// A poller started mid-session must credit the whole visit, not only the part
// it watched, so the connect is dated to when Discord says the activity began.
func TestDiscordTransitionDatesTheVisitFromDiscord(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-2 * time.Hour)
	_, _, occurredAt, _ := discordTransition(DiscordPresence{Playing: true, StartedAt: startedAt}, nil)
	if !occurredAt.Equal(startedAt.UTC()) {
		t.Fatalf("occurredAt = %s, want %s", occurredAt, startedAt.UTC())
	}
	// A missing timestamp falls back to now rather than to the zero time.
	_, _, fallback, _ := discordTransition(DiscordPresence{Playing: true}, nil)
	if time.Since(fallback) > time.Minute {
		t.Fatalf("occurredAt without a Discord start = %s", fallback)
	}
	// A start in the future is Discord disagreeing with our clock; now is the
	// safer of the two, since a future start would report negative playtime.
	_, _, future, _ := discordTransition(DiscordPresence{Playing: true, StartedAt: time.Now().Add(time.Hour)}, nil)
	if time.Since(future) > time.Minute {
		t.Fatalf("occurredAt with a future Discord start = %s", future)
	}
}

// The row has to read like a webhook event so the announcer, dashboard and
// recap keep working without knowing a second writer exists.
func TestDiscordPayloadMirrorsTheWebhookShape(t *testing.T) {
	t.Parallel()
	payload, err := discordPayload(
		discordCharacter{playerName: "Prince Lim", username: "princelim", cid: "QNLLC342"},
		DiscordPresence{DiscordUserID: "1220326041067982941"},
		StatusConnected, time.Unix(0, 0).UTC(), "CR Roleplay",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"name":"Prince Lim"`, `"username":"princelim"`, `"cid":"QNLLC342"`,
		`"discord":"1220326041067982941"`, `"type":"connected"`, `"source":"discord"`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("payload missing %s:\n%s", want, payload)
		}
	}
}
