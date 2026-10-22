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
	return &discordgo.ApplicationCommand{Name: "catalog", Description: "Show partner or SOT internal prices", Contexts: &contexts, Options: []*discordgo.ApplicationCommandOption{{Type: discordgo.ApplicationCommandOptionString, Name: "partner", Description: "Partner code, or SOT for member packages and ala carte prices", Required: true, MaxLength: 50}}}
}

func (b *Bot) handleCatalog(s *discordgo.Session, i *discordgo.Interaction) {
	posting := i.Type == discordgo.InteractionMessageComponent
	partner := ""
	uid, userErr := interactionUserID(i)
	if posting {
		parts := strings.SplitN(i.MessageComponentData().CustomID, ":", 4)
		if len(parts) != 4 || parts[2] != uid || userErr != nil {
			_ = s.InteractionRespond(i, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: "Only the catalog requester can post this result.", Flags: discordgo.MessageFlagsEphemeral}})
			return
		}
		partner = parts[3]
	}
	flags := discordgo.MessageFlagsEphemeral
	responseType := discordgo.InteractionResponseDeferredChannelMessageWithSource
	if posting {
		responseType = discordgo.InteractionResponseDeferredMessageUpdate
		flags = 0
	}
	b.logger.Info("catalog interaction received", "interaction_id", i.ID, "channel_id", i.ChannelID)
	if err := s.InteractionRespond(i, &discordgo.InteractionResponse{Type: responseType, Data: &discordgo.InteractionResponseData{Flags: flags}}); err != nil {
		b.logger.Error("acknowledge catalog interaction", "interaction_id", i.ID, "channel_id", i.ChannelID, "error", err)
		return
	}
	fail := func(message string) { b.orderReply(s, i, message, nil, []discordgo.MessageComponent{}) }
	if userErr != nil {
		fail("Guild member required.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.members.FindByDiscordUserID(ctx, uid); err != nil {
		fail("Registered member required.")
		return
	}
	if !posting {
		for _, opt := range i.ApplicationCommandData().Options {
			if opt.Name == "partner" && opt.Type == discordgo.ApplicationCommandOptionString {
				partner = opt.StringValue()
			}
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
	canonical := ""
	for _, entry := range entries {
		if strings.EqualFold(entry.PartnerCode, strings.TrimSpace(partner)) {
			canonical = entry.PartnerCode
			break
		}
	}
	components := catalogPostButton(uid, canonical, false)
	if posting {
		_, err = s.ChannelMessageSendComplex(i.ChannelID, &discordgo.MessageSend{Embeds: *response.Embeds, Files: response.Files, AllowedMentions: &discordgo.MessageAllowedMentions{}})
		if err != nil {
			b.logger.Error("post catalog", "channel_id", i.ChannelID, "user_id", uid, "error", err)
			fail("Could not post catalog. Check channel permissions and retry.")
			return
		}
		components = catalogPostButton(uid, canonical, true)
		content := "Catalog posted to this channel."
		response.Content = &content
		response.Files = nil
		b.logger.Info("catalog posted", "channel_id", i.ChannelID, "user_id", uid, "partner", canonical)
	}
	response.Components = &components
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
	sections := []string{"sale", "purchase"}
	if strings.EqualFold(code, "SOT") {
		sections = []string{"package", "weapon", "ammo", "body_drugs", "other"}
	}
	for _, direction := range sections {
		heading, label, color := "SOT sells to partner", "Sales", 0x57F287
		if direction == "purchase" {
			heading, label, color = "SOT buys from partner", "Purchases", 0xED4245
		}
		if strings.EqualFold(code, "SOT") {
			heading, color = "SOT member prices", 0xF1C40F
			label = map[string]string{"package": "Member packages", "weapon": "À la carte · Weapons", "ammo": "À la carte · Ammo", "body_drugs": "À la carte · Body & Drugs", "other": "À la carte · Other"}[direction]
		}
		embed := &discordgo.MessageEmbed{Description: heading, Color: color, Footer: &discordgo.MessageEmbedFooter{Text: "Current database prices"}}
		fmt.Fprintf(&text, "%s\n", heading)
		for _, e := range entries {
			matches := e.Direction == direction
			if strings.EqualFold(code, "SOT") {
				group := e.StockGroup
				if group != "weapon" && group != "ammo" && group != "body_drugs" {
					group = "other"
				}
				if e.Unit == "package" {
					group = "package"
				}
				matches = e.Direction == "internal" && group == direction
			}
			if !strings.EqualFold(e.PartnerCode, code) || !matches {
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
			if strings.EqualFold(code, "SOT") {
				continue
			}
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

func catalogPostButton(userID, code string, posted bool) []discordgo.MessageComponent {
	label := "Post Result"
	if posted {
		label = "Posted"
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.Button{Label: label, Style: discordgo.SuccessButton, CustomID: "catalog:post:" + userID + ":" + code, Disabled: posted}}}}
}
