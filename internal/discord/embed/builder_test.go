package embed

import (
	"testing"
	"time"
)

func TestEmbedBuilder(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, time.August, 13, 8, 30, 0, 0, time.FixedZone("WIB", 7*60*60))
	builder := New("Profile").
		Description("Discord profile").
		Color(0x5865F2).
		Field("Name", "Captain", true).
		Thumbnail("https://example.com/avatar.png").
		Image("https://example.com/image.png").
		Footer("SOT", "https://example.com/icon.png").
		Timestamp(timestamp)

	embed := builder.Build()
	if embed.Title != "Profile" || embed.Description != "Discord profile" || embed.Color != 0x5865F2 {
		t.Errorf("unexpected embed: %#v", embed)
	}
	if len(embed.Fields) != 1 || embed.Fields[0].Name != "Name" || !embed.Fields[0].Inline {
		t.Errorf("unexpected fields: %#v", embed.Fields)
	}
	if embed.Thumbnail == nil || embed.Thumbnail.URL == "" || embed.Image == nil || embed.Image.URL == "" {
		t.Error("embed media is missing")
	}
	if embed.Footer == nil || embed.Footer.Text != "SOT" {
		t.Error("embed footer is missing")
	}
	if embed.Timestamp != "2026-08-13T01:30:00Z" {
		t.Errorf("Timestamp = %q", embed.Timestamp)
	}

	builder.Field("Playtime", "1:04", false)
	if len(embed.Fields) != 1 {
		t.Error("built embed changed after builder mutation")
	}
}

func TestEmbedBuilderSkipsEmptyOptionalValues(t *testing.T) {
	t.Parallel()

	embed := New("Empty").Thumbnail("").Image("").Footer("", "").Timestamp(time.Time{}).Build()
	if embed.Thumbnail != nil || embed.Image != nil || embed.Footer != nil || embed.Timestamp != "" {
		t.Errorf("empty optional values were rendered: %#v", embed)
	}
}
