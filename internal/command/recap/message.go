package recap

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
)

const (
	Command              = "recap"
	CheckCommand         = "check"
	maxDescriptionLength = 4096
)

func CheckEmbed(currentMember member.Member, recaps []member.PlaytimeRecap, attendanceStart, now time.Time, requiredPlaytime time.Duration) *discordgo.MessageEmbed {
	playtime := time.Duration(0)
	for _, recap := range recaps {
		if recap.MemberID == currentMember.ID {
			playtime = recap.Playtime
			break
		}
	}

	attended := 0
	for _, recap := range recaps {
		if recap.Playtime > requiredPlaytime {
			attended++
		}
	}

	characterName := strings.TrimSpace(currentMember.CharacterName)
	if characterName == "" {
		characterName = currentMember.DisplayName
	}
	status := "Not Attended"
	color := 0xFEE75C
	if playtime > requiredPlaytime {
		status = "Attended"
		color = 0x57F287
	}

	return embed.New(fmt.Sprintf("Attendance Check (%s)", attendanceStart.Format("02 January 2006"))).
		Description(fmt.Sprintf("Minimum playtime: %s", formatPlaytime(requiredPlaytime))).
		Color(color).
		Field("Name", fmt.Sprintf("%s (<@%s>)", escapeMarkdown(characterName), currentMember.UserID), false).
		Field("Playtime", formatPlaytime(playtime), true).
		Field("Status", status, true).
		Footer(fmt.Sprintf("Attended: %d • Not attending: %d • Participants: %d", attended, len(recaps)-attended, len(recaps)), "").
		Timestamp(now).
		Build()
}

func AttendanceWindow(now time.Time, startTime, endTime time.Duration, location *time.Location) (time.Time, time.Time) {
	now = now.In(location)
	hour := int(startTime / time.Hour)
	minute := int(startTime%time.Hour) / int(time.Minute)
	start := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, location)
	if start.After(now) {
		start = start.AddDate(0, 0, -1)
	}
	endHour := int(endTime / time.Hour)
	endMinute := int(endTime%time.Hour) / int(time.Minute)
	end := time.Date(start.Year(), start.Month(), start.Day(), endHour, endMinute, 0, 0, location)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	if now.Before(end) {
		end = now
	}
	return start, end
}

func Embed(recaps []member.PlaytimeRecap, attendanceStart, now time.Time, requiredPlaytime time.Duration) *discordgo.MessageEmbed {
	attended := make([]member.PlaytimeRecap, 0, len(recaps))
	notAttending := make([]member.PlaytimeRecap, 0, len(recaps))
	for _, recap := range recaps {
		if recap.Playtime > requiredPlaytime {
			attended = append(attended, recap)
		} else {
			notAttending = append(notAttending, recap)
		}
	}

	var description strings.Builder
	fmt.Fprintf(&description, "Minimum playtime: %s\n\n", formatPlaytime(requiredPlaytime))
	appendSection(&description, "✅ Attended", attended)
	description.WriteString("\n")
	appendSection(&description, "❌ Not Attending", notAttending)

	title := fmt.Sprintf("Attendance Recap (%s)", attendanceStart.Format("02 January 2006"))
	return embed.New(title).
		Description(truncateDescription(description.String())).
		Color(0x5865F2).
		Footer(fmt.Sprintf("Attended: %d • Not attending: %d • Participants: %d", len(attended), len(notAttending), len(recaps)), "").
		Timestamp(now).
		Build()
}

func appendSection(description *strings.Builder, title string, recaps []member.PlaytimeRecap) {
	description.WriteString("**" + title + "**\n")
	if len(recaps) == 0 {
		description.WriteString("-\n")
		return
	}
	for index, recap := range recaps {
		line := fmt.Sprintf("%d. %s (<@%s>) - %s\n",
			index+1,
			escapeMarkdown(recap.CharacterName),
			recap.UserID,
			formatPlaytime(recap.Playtime),
		)
		description.WriteString(line)
	}
}

func truncateDescription(description string) string {
	if len(description) <= maxDescriptionLength {
		return description
	}

	const suffix = "More members omitted."
	limit := maxDescriptionLength - len(suffix) - 1
	cut := strings.LastIndex(description[:limit], "\n")
	if cut < 0 {
		cut = limit
	}
	return description[:cut] + "\n" + suffix
}

func formatPlaytime(duration time.Duration) string {
	duration = duration.Truncate(time.Minute)
	hours := int(duration / time.Hour)
	minutes := int(duration%time.Hour) / int(time.Minute)
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func escapeMarkdown(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "~", "\\~", "|", "\\|")
	return replacer.Replace(value)
}
