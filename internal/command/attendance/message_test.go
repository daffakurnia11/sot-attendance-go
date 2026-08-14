package attendance

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestAnnouncement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 13, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		command     string
		title       string
		description string
		color       int
	}{
		{command: AttendanceStart, title: "Attendance Started", description: "Attendance is now open! Let's play **CR Roleplay** now!", color: 0x57F287},
		{command: AttendanceEnd, title: "Attendance Ended", description: "Attendance is now closed. Thank you!", color: 0xED4245},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			t.Parallel()
			message := Announcement(tt.command, "CR Roleplay", now)
			if message.Content != "@here" {
				t.Errorf("Content = %q", message.Content)
			}
			if message.AllowedMentions == nil || len(message.AllowedMentions.Parse) != 1 ||
				message.AllowedMentions.Parse[0] != discordgo.AllowedMentionTypeEveryone {
				t.Errorf("AllowedMentions = %#v", message.AllowedMentions)
			}
			if len(message.Embeds) != 1 || message.Embeds[0].Title != tt.title || message.Embeds[0].Color != tt.color {
				t.Fatalf("unexpected embed: %#v", message.Embeds)
			}
			if message.Embeds[0].Description != tt.description {
				t.Errorf("Description = %q", message.Embeds[0].Description)
			}
			if len(message.Embeds[0].Fields) != 0 {
				t.Errorf("time fields must be removed: %#v", message.Embeds[0].Fields)
			}
			if message.Embeds[0].Timestamp != now.Format(time.RFC3339) {
				t.Errorf("Timestamp = %q", message.Embeds[0].Timestamp)
			}
		})
	}
}
