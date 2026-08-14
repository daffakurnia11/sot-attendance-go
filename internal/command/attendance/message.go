package attendance

import (
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
)

const (
	AttendanceStart = "attendance-start"
	AttendanceEnd   = "attendance-end"
)

func Announcement(command, serverName string, announcedAt time.Time) *discordgo.MessageSend {
	title := "Attendance Started"
	description := "Attendance is now open! Let's play **" + serverName + "** now!"
	color := 0x57F287
	if command == AttendanceEnd {
		title = "Attendance Ended"
		description = "Attendance is now closed. Thank you!"
		color = 0xED4245
	}

	embed := embed.New(title).
		Description(description).
		Color(color).
		Footer("SOT Attendance", "").
		Timestamp(announcedAt).
		Build()

	return &discordgo.MessageSend{
		Content: "@here",
		Embeds:  []*discordgo.MessageEmbed{embed},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{discordgo.AllowedMentionTypeEveryone},
		},
	}
}
