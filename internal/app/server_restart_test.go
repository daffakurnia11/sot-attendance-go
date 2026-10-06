package app

import (
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"testing"
	"time"
)

func TestRestartBetween(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	parse := func(s string) time.Time {
		v, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, tc := range []struct{ from, to, want string }{
		{"2026-10-06T04:59:59+07:00", "2026-10-06T05:00:04+07:00", "2026-10-06T05:00:00+07:00"},
		{"2026-10-06T16:59:59+07:00", "2026-10-06T17:00:00+07:00", "2026-10-06T17:00:00+07:00"},
		{"2026-10-06T17:00:00+07:00", "2026-10-06T17:00:05+07:00", ""},
		{"2026-10-06T23:59:59+07:00", "2026-10-07T05:01:00+07:00", "2026-10-07T05:00:00+07:00"},
	} {
		got := restartBetween(parse(tc.from), parse(tc.to), []time.Duration{5 * time.Hour, 17 * time.Hour}, loc)
		if tc.want == "" {
			if !got.IsZero() {
				t.Fatalf("unexpected %v", got)
			}
		} else if !got.Equal(parse(tc.want)) {
			t.Fatalf("got %v want %s", got, tc.want)
		}
	}
}

func TestRestartBlocksStaleActivity(t *testing.T) {
	at := time.Now()
	old := at.Add(-time.Hour)
	fresh := at.Add(time.Second)
	b := &Bot{restartBoundary: at, restartCleared: map[string]bool{}}
	entry := presence.MemberPresence{DiscordUserID: "1", Status: "online", Playing: true, StartedAt: &old}
	if !b.staleRestartActivity(entry) {
		t.Fatal("old activity reopened")
	}
	entry.StartedAt = &fresh
	if b.staleRestartActivity(entry) {
		t.Fatal("new activity blocked")
	}
	entry.StartedAt = nil
	if !b.staleRestartActivity(entry) {
		t.Fatal("unknown activity reopened")
	}
	entry.Playing = false
	entry.Status = "offline"
	b.staleRestartActivity(entry)
	entry.Playing = true
	if !b.staleRestartActivity(entry) {
		t.Fatal("offline cleared restart guard")
	}
	entry.Playing = false
	entry.Status = "online"
	b.staleRestartActivity(entry)
	entry.Playing = true
	if b.staleRestartActivity(entry) {
		t.Fatal("fresh activity after visible stop blocked")
	}
}
