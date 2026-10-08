package app

import (
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"time"
)

func restartBetween(from, to time.Time, clocks []time.Duration, location *time.Location) time.Time {
	var latest time.Time
	local := to.In(location)
	for _, day := range []time.Time{local.AddDate(0, 0, -1), local} {
		midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
		for _, clock := range clocks {
			at := midnight.Add(clock)
			if at.After(from) && !at.After(to) && at.After(latest) {
				latest = at
			}
		}
	}
	return latest
}

// Serialized with Discord recording by the bot's main loop.
func (b *Bot) checkServerRestart(ctx context.Context, now time.Time) {
	if !b.announces || b.settings == nil || b.serverLogs == nil {
		return
	}
	request, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	clocks, err := b.settings.LoadRestartSchedule(request)
	if err != nil {
		b.logger.Error("load server restart schedule", "error", err)
		return
	}
	at := restartBetween(b.restartChecked, now, clocks, b.location)
	if !at.IsZero() {
		previous := restartBetween(at.Add(-48*time.Hour), at.Add(-time.Nanosecond), clocks, b.location)
		count, err := b.serverLogs.CloseForRestart(request, at, previous)
		if err != nil {
			b.logger.Error("record scheduled restart exits", "error", err)
			return
		}
		b.restartBoundary = at
		b.restartCleared = make(map[string]bool)
		b.logger.Info("scheduled restart exits recorded", "restart_at", at, "transitions", count)
	}
	b.restartChecked = now
}

func (b *Bot) staleRestartActivity(entry presence.MemberPresence) bool {
	if b.restartBoundary.IsZero() {
		return false
	}
	if !entry.Playing {
		if entry.Status == "online" || entry.Status == "idle" || entry.Status == "dnd" {
			b.restartCleared[entry.DiscordUserID] = true
		}
		return false
	}
	if entry.StartedAt != nil {
		return !entry.StartedAt.After(b.restartBoundary)
	}
	return !b.restartCleared[entry.DiscordUserID]
}
