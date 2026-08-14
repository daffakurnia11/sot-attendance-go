package profile

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestBuildProfileAndEmbed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 1, 30, 0, 0, time.UTC)
	member := &discordgo.Member{Nick: "Captain", User: &discordgo.User{ID: "123", Username: "pirate", Avatar: "hash"}}
	startedAt := now.Add(-90*time.Minute - 5*time.Second)
	activity := &discordgo.Activity{Timestamps: discordgo.TimeStamps{StartTimestamp: startedAt.UnixMilli()}}
	profile := Build(member, activity, now)
	if profile.Name != "Captain" || profile.Username != "pirate" || profile.Activity != "1:30:05" {
		t.Errorf("unexpected profile: %#v", profile)
	}
	embed := Embed(profile)
	if len(embed.Fields) != 4 {
		t.Fatalf("field count = %d, want 4", len(embed.Fields))
	}
	if profile.StartTime != "<t:1786579195:F>" {
		t.Errorf("StartTime = %q", profile.StartTime)
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL == "" {
		t.Error("avatar thumbnail is missing")
	}
	for _, field := range embed.Fields {
		if field.Name == "Avatar" {
			t.Error("avatar field must not be rendered")
		}
	}
}

func TestBuildProfileWithoutPresence(t *testing.T) {
	t.Parallel()
	member := &discordgo.Member{User: &discordgo.User{ID: "123", Username: "pirate"}}
	profile := Build(member, nil, time.Time{})
	if got := profile.Activity; got != "Not Played" {
		t.Errorf("Activity = %q", got)
	}
	if len(Embed(profile).Fields) != 3 {
		t.Error("not-playing embed must not show start time")
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()
	if got := formatDuration(27*time.Hour + 2*time.Minute + 3*time.Second); got != "27:02:03" {
		t.Errorf("formatDuration() = %q", got)
	}
	if got := formatDuration(4*time.Minute + 3*time.Second); got != "4:03" {
		t.Errorf("formatDuration() = %q", got)
	}
}

func TestBuildProfileMissingDiscordStartTime(t *testing.T) {
	t.Parallel()
	member := &discordgo.Member{User: &discordgo.User{ID: "123", Username: "pirate"}}
	profile := Build(member, &discordgo.Activity{}, time.Now())
	if profile.Activity != "Unavailable" || profile.StartTime != "" || !profile.Playing {
		t.Errorf("unexpected profile: %#v", profile)
	}
}
