package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
)

func catalogSlashCommand(contexts []discordgo.InteractionContextType) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{Name: "catalog", Description: "Show a partner's current sale and purchase prices", Contexts: &contexts, Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "partner", Description: "Partner code", Required: true, MaxLength: 50}, {Type: discordgo.ApplicationCommandOptionBoolean, Name: "public", Description: "Show catalog to everyone in this channel (default: private)"}}}
}

func (b *Bot) handleCatalog(s *discordgo.Session, i *discordgo.Interaction) {
	b.logger.Info("catalog interaction received", "interaction_id", i.ID, "channel_id", i.ChannelID)
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Flags: catalogResponseFlags(i.ApplicationCommandData().Options)}}); err != nil {
		b.logger.Error("acknowledge catalog interaction", "interaction_id", i.ID, "channel_id", i.ChannelID, "error", err)
		return
	}
	fail := func(message string) { b.orderReply(s, i, message, nil, []discordgo.MessageComponent{}) }
	uid, err := interactionUserID(i)
	if err != nil {
		fail("Guild member required.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = b.members.FindByDiscordUserID(ctx, uid); err != nil {
		fail("Registered member required.")
		return
	}
	opts := i.ApplicationCommandData().Options
	partner := ""
	for _, opt := range opts {
		if opt.Name == "partner" && opt.Type == discordgo.ApplicationCommandOptionString {
			partner = opt.StringValue()
		}
	}
	if strings.TrimSpace(partner) == "" {
		fail("Partner code required. Example: /catalog partner:GWF")
		return
	}
	entries, err := business.NewRepository(b.database).List(ctx)
	if err != nil {
		b.logger.Error("load partner catalog", "error", err)
		fail("Could not load catalog. Retry shortly.")
		return
	}
	response, err := partnerCatalogResponse(entries, partner)
	if err != nil {
		fail(err.Error())
		return
	}
	if _, err = s.InteractionResponseEdit(i, response); err != nil {
		b.logger.Error("respond partner catalog", "error", err)
	}
}

func partnerCatalogResponse(entries []business.Entry, code string) (*discordgo.WebhookEdit, error) {
	code = strings.TrimSpace(code)
	name := ""
	var text strings.Builder
	embeds := []*discordgo.MessageEmbed{}
	size := 0
	oversized := false
	for _, direction := range []string{"sale", "purchase"} {
		heading, label, color := "SOT sells to partner", "Sales", 0x57F287
		if direction == "purchase" {
			heading, label, color = "SOT buys from partner", "Purchases", 0xED4245
		}
		embed := &discordgo.MessageEmbed{Description: heading, Color: color, Footer: &discordgo.MessageEmbedFooter{Text: "Current database prices"}}
		fmt.Fprintf(&text, "%s\n", heading)
		for _, e := range entries {
			if !strings.EqualFold(e.PartnerCode, code) || e.Direction != direction {
				continue
			}
			name = e.PartnerCode
			value := fmt.Sprintf("**%s** / %s", orderMoney(e.Price), e.Unit)
			if e.Notes != "" {
				value += "\n" + e.Notes
			}
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: e.Name, Value: value, Inline: false})
			fmt.Fprintf(&text, "%s — %s / %s\n%s\n\n", e.Name, orderMoney(e.Price), e.Unit, e.Notes)
			size += len([]rune(e.Name)) + len([]rune(value))
			if len([]rune(e.Name)) > 256 || len([]rune(value)) > 1024 || len(embed.Fields) > 25 {
				oversized = true
			}
		}
		if len(embed.Fields) == 0 {
			embed.Description += "\nNo products listed."
		}
		embed.Title = label
		embeds = append(embeds, embed)
	}

	if name == "" {
		return nil, fmt.Errorf("Unknown partner code or no catalog available.")
	}
	for _, embed := range embeds {
		embed.Title = name + " · " + embed.Title
	}
	edit := &discordgo.WebhookEdit{Embeds: &embeds, AllowedMentions: &discordgo.MessageAllowedMentions{}}
	// Large catalogs remain complete without exceeding Discord embed limits.
	if oversized || size > 2000 {
		edit.Files = []*discordgo.File{{Name: "catalog.txt", ContentType: "text/plain", Reader: strings.NewReader(text.String())}}
		edit.Embeds = &[]*discordgo.MessageEmbed{{Title: name + " catalog", Description: "Full catalog attached."}}
	}
	return edit, nil
}

func catalogResponseFlags(options []*discordgo.ApplicationCommandInteractionDataOption) discordgo.MessageFlags {
	for _, option := range options {
		if option.Name == "public" && option.Type == discordgo.ApplicationCommandOptionBoolean && option.BoolValue() {
			return 0
		}
	}
	return discordgo.MessageFlagsEphemeral
}
