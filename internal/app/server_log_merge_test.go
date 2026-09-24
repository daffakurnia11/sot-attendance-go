package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

// mergeStep is one stored row reaching the announcer, and what the channel
// should see as a result.
type mergeStep struct {
	source      string
	status      string
	at          time.Time
	serverID    string
	trustedOpen *time.Time
	want        announcementAction
	wantSources []string
	wantSlot    string
}

func replayMerge(t *testing.T, steps []mergeStep) {
	t.Helper()
	var merger announcementMerger
	for index, step := range steps {
		announcement := serverlog.Announcement{
			ID: int64(index + 1), ServerMemberID: 7, Status: step.status, OccurredAt: step.at,
			Source: step.source, TrustedOpenSince: step.trustedOpen,
		}
		event := presence.ServerLogEvent{Status: step.status, OccurredAt: step.at, Source: step.source, ServerID: step.serverID}
		action, rendered, _ := merger.plan(announcement, event)
		if action != step.want {
			t.Fatalf("step %d (%s %s): action = %d, want %d", index, step.source, step.status, action, step.want)
		}
		if action == announceSkip {
			continue
		}
		if step.wantSources != nil && !reflect.DeepEqual(rendered.Sources, step.wantSources) {
			t.Errorf("step %d: sources = %v, want %v", index, rendered.Sources, step.wantSources)
		}
		if rendered.ServerID != step.wantSlot {
			t.Errorf("step %d: slot = %q, want %q", index, rendered.ServerID, step.wantSlot)
		}
		merger.record(announcement, rendered, "message")
	}
}

// Image 1: the webhook reports connecting first and Discord follows, then
// Discord sees the arrival first and the webhook follows. Two messages, not
// four, and the connected one ends up carrying the webhook's slot.
func TestAnnouncementMergerFoldsAConnectFromBothWitnesses(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 23, 16, 48, 0, 0, time.UTC)
	serverOpen := base
	replayMerge(t, []mergeStep{
		{source: "server", status: "connecting", at: base, want: announcePost},
		{source: "discord", status: "connecting", at: base.Add(10 * time.Second), trustedOpen: &serverOpen,
			want: announceEdit, wantSources: []string{"server", "discord"}},
		{source: "discord", status: "connected", at: base.Add(2 * time.Minute), trustedOpen: &serverOpen, want: announcePost},
		{source: "server", status: "connected", at: base.Add(2*time.Minute + 20*time.Second), serverID: "763",
			want: announceEdit, wantSources: []string{"server", "discord"}, wantSlot: "763"},
	})
}

// Image 2: the webhook reports the exit first and Discord follows seconds
// later. One message, the webhook's details kept.
func TestAnnouncementMergerFoldsAnExitFromBothWitnesses(t *testing.T) {
	t.Parallel()
	exit := time.Date(2026, 9, 23, 18, 3, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "server", status: "disconnected", at: exit, serverID: "735", want: announcePost, wantSlot: "735"},
		{source: "discord", status: "disconnected", at: exit.Add(15 * time.Second),
			want: announceEdit, wantSources: []string{"server", "discord"}, wantSlot: "735"},
	})
}

// Case 2: Discord activity switched off, or the member gone invisible, while
// the webhook still has them on the server. Not an exit.
func TestAnnouncementMergerDropsAWeakerExitWhileTheWebhookHasThePlayer(t *testing.T) {
	t.Parallel()
	connected := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "server", status: "connected", at: connected, want: announcePost},
		{source: "discord", status: "disconnected", at: connected.Add(time.Hour), trustedOpen: &connected, want: announceSkip},
	})
}

// Activity switched back on an hour into a visit the webhook reported is not
// an arrival either.
func TestAnnouncementMergerDropsALateWeakerConnect(t *testing.T) {
	t.Parallel()
	connected := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "server", status: "connected", at: connected, want: announcePost},
		{source: "discord", status: "connected", at: connected.Add(time.Hour), trustedOpen: &connected, want: announceSkip},
	})
}

// Cases 3 and 4: with the webhook silent, the weaker witnesses announce on
// their own, and the CFX slot wins over Discord's lack of one.
func TestAnnouncementMergerAnnouncesWitnessesWhenTheWebhookIsSilent(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "discord", status: "connected", at: base, want: announcePost},
		{source: "cfx", status: "connected", at: base.Add(3 * time.Minute), serverID: "42",
			want: announceEdit, wantSources: []string{"cfx", "discord"}, wantSlot: "42"},
	})
}

// The same witness twice is two visits - a quick reconnect - not one.
func TestAnnouncementMergerPostsARepeatFromTheSameWitness(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "server", status: "connected", at: base, want: announcePost},
		{source: "server", status: "connected", at: base.Add(5 * time.Minute), want: announcePost},
	})
}

// Reports further apart than the window are separate changes.
func TestAnnouncementMergerKeepsDistantReportsApart(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	replayMerge(t, []mergeStep{
		{source: "server", status: "disconnected", at: base, want: announcePost},
		{source: "discord", status: "disconnected", at: base.Add(announcementMergeWindow + time.Second), want: announcePost},
	})
}
