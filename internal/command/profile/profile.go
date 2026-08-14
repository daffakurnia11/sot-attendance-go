package profile

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
)

type Profile struct {
	Name       string
	Username   string
	Activity   string
	StartTime  string
	Playing    bool
	AvatarURL  string
	ObservedAt time.Time
}

func Build(member *discordgo.Member, activity *discordgo.Activity, observedAt time.Time) Profile {
	playtime := "Not Played"
	startTime := ""
	playing := activity != nil
	if playing {
		playtime = "Unavailable"
		startTimestamp := activity.Timestamps.StartTimestamp
		if startTimestamp > 0 {
			startedAt := time.UnixMilli(startTimestamp)
			if !startedAt.After(observedAt) {
				playtime = formatDuration(observedAt.Sub(startedAt))
				startTime = fmt.Sprintf("<t:%d:F>", startedAt.Unix())
			}
		}
	}

	return Profile{
		Name: member.DisplayName(), Username: member.User.Username,
		Activity: playtime, StartTime: startTime, Playing: playing,
		AvatarURL: member.AvatarURL("256"), ObservedAt: observedAt.UTC(),
	}
}

func Embed(profile Profile) *discordgo.MessageEmbed {
	builder := embed.New("CR Roleplay Profile").
		Color(0x5865F2).
		Field("Name", profile.Name, true).
		Field("Username", profile.Username, true).
		Field("Playtime", profile.Activity, false).
		Thumbnail(profile.AvatarURL).
		Timestamp(profile.ObservedAt)

	if profile.Playing {
		startTime := profile.StartTime
		if startTime == "" {
			startTime = "Unavailable"
		}
		builder.Field("Start time", startTime, false)
	}

	return builder.Build()
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	duration = duration.Truncate(time.Second)
	hours := int(duration / time.Hour)
	minutes := int(duration%time.Hour) / int(time.Minute)
	seconds := int(duration%time.Minute) / int(time.Second)
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
