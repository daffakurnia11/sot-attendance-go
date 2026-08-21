package recap

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

func TestAttendanceWindow(t *testing.T) {
	location := time.FixedZone("Asia/Jakarta", 7*60*60)
	startTime := 21 * time.Hour
	endTime := time.Hour

	duringOvernight := time.Date(2026, 8, 14, 0, 30, 0, 0, location)
	wantStart := time.Date(2026, 8, 13, 21, 0, 0, 0, location)
	start, end := AttendanceWindow(duringOvernight, startTime, endTime, location)
	if !start.Equal(wantStart) || !end.Equal(duringOvernight) {
		t.Fatalf("AttendanceWindow() = (%v, %v), want (%v, %v)", start, end, wantStart, duringOvernight)
	}

	afterEnd := time.Date(2026, 8, 14, 10, 0, 0, 0, location)
	wantEnd := time.Date(2026, 8, 14, 1, 0, 0, 0, location)
	start, end = AttendanceWindow(afterEnd, startTime, endTime, location)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("AttendanceWindow() = (%v, %v), want (%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestEmbedTruncatesLargeRecapWithinDiscordLimit(t *testing.T) {
	now := time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC)
	recaps := make([]member.PlaytimeRecap, 500)
	for index := range recaps {
		recaps[index] = member.PlaytimeRecap{
			UserID:        fmt.Sprintf("100%03d", index),
			DisplayName:   fmt.Sprintf("Discord member %03d with a long display name", index),
			CharacterName: fmt.Sprintf("Character %03d with a long roleplay name", index),
			Playtime:      2 * time.Hour,
		}
	}

	embed := Embed(recaps, now.Add(-2*time.Hour), now, 90*time.Minute)
	if len(embed.Description) > maxDescriptionLength {
		t.Fatalf("Embed() description length = %d, want <= %d", len(embed.Description), maxDescriptionLength)
	}
	if !strings.HasSuffix(embed.Description, "More members omitted.") {
		t.Fatalf("Embed() description must report truncation, got suffix %q", embed.Description[len(embed.Description)-40:])
	}
}

func TestEmbedShowsRankedPlaytime(t *testing.T) {
	now := time.Date(2026, 8, 13, 23, 0, 0, 0, time.UTC)
	embed := Embed([]member.PlaytimeRecap{{
		UserID: "123", DisplayName: "Delta*Kilo", CharacterName: "John_Doe", Playtime: 2*time.Hour + 5*time.Minute,
	}, {
		UserID: "456", DisplayName: "Mici", CharacterName: "Mici Yu", Playtime: 90 * time.Minute,
	}}, now.Add(-2*time.Hour), now, 90*time.Minute)

	if embed.Title != "Attendance Recap (13 August 2026)" {
		t.Fatalf("Embed() title = %q", embed.Title)
	}
	if strings.Contains(embed.Description, "Attendance started") || strings.Contains(embed.Description, "<t:") {
		t.Fatalf("Embed() must not show attendance start timestamp: %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "1. John\\_Doe (<@123>) - 2h5m") {
		t.Fatalf("Embed() description = %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "❌ Not Attending**\n1. Mici Yu (<@456>) - 1h30m") {
		t.Fatalf("Embed() strict threshold description = %q", embed.Description)
	}
	if embed.Footer == nil || embed.Footer.Text != "Attended: 1 • Not attending: 1 • Participants: 2" {
		t.Fatalf("Embed() footer = %#v", embed.Footer)
	}
}

func TestCheckEmbedShowsCurrentMemberStatusAndGlobalSummary(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 24, 0, 0, time.FixedZone("Asia/Jakarta", 7*60*60))
	currentMember := member.Member{
		ID: 2, UserID: "123456789", Username: "prince", DisplayName: "Prince Lim", CharacterName: "Prince Nakamura",
	}
	recaps := []member.PlaytimeRecap{
		{MemberID: 1, Playtime: 2 * time.Hour},
		{MemberID: 2, Playtime: 49 * time.Minute},
	}

	embed := CheckEmbed(currentMember, recaps, now.Add(-6*time.Hour), now, time.Hour)
	if embed.Title != "Attendance Check (20 August 2026)" || embed.Description != "Minimum playtime: 1h0m" {
		t.Fatalf("unexpected header: %#v", embed)
	}
	if len(embed.Fields) != 3 || embed.Fields[0].Name != "Name" || embed.Fields[0].Value != "Prince Nakamura (<@123456789>)" {
		t.Fatalf("unexpected identity fields: %#v", embed.Fields)
	}
	if embed.Fields[1].Value != "49m" || embed.Fields[2].Value != "Not Attended" || embed.Color != 0xFEE75C {
		t.Fatalf("unexpected attendance result: %#v", embed)
	}
	if embed.Footer == nil || embed.Footer.Text != "Attended: 1 • Not attending: 1 • Participants: 2" {
		t.Fatalf("unexpected footer: %#v", embed.Footer)
	}
	if embed.Timestamp != now.UTC().Format(time.RFC3339) {
		t.Fatalf("Timestamp = %q", embed.Timestamp)
	}
}

func TestCheckEmbedUsesDisplayNameFallbackAndZeroPlaytime(t *testing.T) {
	now := time.Date(2026, 8, 21, 3, 24, 0, 0, time.UTC)
	currentMember := member.Member{ID: 3, UserID: "987654321", Username: "delta", DisplayName: "Delta*Kilo"}

	embed := CheckEmbed(currentMember, nil, now, now, time.Hour)
	if embed.Fields[0].Value != "Delta\\*Kilo (<@987654321>)" || embed.Fields[1].Value != "0m" || embed.Fields[2].Value != "Not Attended" {
		t.Fatalf("unexpected fallback result: %#v", embed.Fields)
	}
}
