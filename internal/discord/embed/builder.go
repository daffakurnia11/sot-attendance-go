package embed

import (
	"time"

	"github.com/bwmarrin/discordgo"
)

// EmbedBuilder builds Discord embeds while keeping command presentation concise.
type EmbedBuilder struct {
	embed discordgo.MessageEmbed
}

func New(title string) *EmbedBuilder {
	return &EmbedBuilder{embed: discordgo.MessageEmbed{Title: title}}
}

func (b *EmbedBuilder) Description(description string) *EmbedBuilder {
	b.embed.Description = description
	return b
}

func (b *EmbedBuilder) Color(color int) *EmbedBuilder {
	b.embed.Color = color
	return b
}

func (b *EmbedBuilder) Field(name, value string, inline bool) *EmbedBuilder {
	b.embed.Fields = append(b.embed.Fields, &discordgo.MessageEmbedField{
		Name: name, Value: value, Inline: inline,
	})
	return b
}

func (b *EmbedBuilder) Thumbnail(url string) *EmbedBuilder {
	if url != "" {
		b.embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: url}
	}
	return b
}

func (b *EmbedBuilder) Image(url string) *EmbedBuilder {
	if url != "" {
		b.embed.Image = &discordgo.MessageEmbedImage{URL: url}
	}
	return b
}

func (b *EmbedBuilder) Footer(text, iconURL string) *EmbedBuilder {
	if text != "" || iconURL != "" {
		b.embed.Footer = &discordgo.MessageEmbedFooter{Text: text, IconURL: iconURL}
	}
	return b
}

func (b *EmbedBuilder) Timestamp(timestamp time.Time) *EmbedBuilder {
	if !timestamp.IsZero() {
		b.embed.Timestamp = timestamp.UTC().Format(time.RFC3339)
	}
	return b
}

// Build returns an independent embed. Later builder changes do not mutate it.
func (b *EmbedBuilder) Build() *discordgo.MessageEmbed {
	embed := b.embed
	embed.Fields = append([]*discordgo.MessageEmbedField(nil), b.embed.Fields...)
	return &embed
}
