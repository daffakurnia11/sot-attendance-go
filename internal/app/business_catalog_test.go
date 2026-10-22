package app

import (
	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestPartnerCatalog(t *testing.T) {
	entries := []business.Entry{{PartnerCode: "GWF", Direction: "sale", Name: "Vector", Price: 9000, Unit: "piece"}, {PartnerCode: "GWF", Direction: "purchase", Name: "Vest", Price: 2800, Unit: "piece", Notes: "Unlimited"}, {PartnerCode: "DW", Direction: "sale", Name: "Hidden", Price: 1}}
	edit, err := partnerCatalogResponse(entries, " gwf ")
	if err != nil {
		t.Fatal(err)
	}
	if len(*edit.Embeds) != 2 {
		t.Fatal("sale and purchase catalogs not separated")
	}
	sale, purchase := (*edit.Embeds)[0], (*edit.Embeds)[1]
	if sale.Title != "GWF · Sales" || purchase.Title != "GWF · Purchases" || sale.Color == purchase.Color {
		t.Fatal("directions not clearly labeled")
	}
	if len(sale.Fields) != 1 || sale.Fields[0].Name != "Vector" || sale.Fields[0].Inline || sale.Fields[0].Value != "**$9,000** / piece" {
		t.Fatalf("incorrect sale layout: %+v", sale.Fields)
	}
	if len(purchase.Fields) != 1 || purchase.Fields[0].Name != "Vest" || purchase.Fields[0].Value != "**$2,800** / piece\nUnlimited" {
		t.Fatal("purchase price/notes not grouped")
	}
	entries[0].Price = 10000
	edit, _ = partnerCatalogResponse(entries, "GWF")
	if !strings.Contains((*edit.Embeds)[0].Fields[0].Value, "$10,000") {
		t.Fatal("stale price")
	}
	if _, err = partnerCatalogResponse(entries, "missing"); err == nil {
		t.Fatal("accepted unknown partner")
	}
	entries[0].Notes = strings.Repeat("x", 5000)
	edit, err = partnerCatalogResponse(entries, "GWF")
	if err != nil || len(edit.Files) != 1 {
		t.Fatal("large catalog not attached")
	}
	full, _ := io.ReadAll(edit.Files[0].Reader)
	if !strings.Contains(string(full), entries[0].Notes) {
		t.Fatal("catalog truncated")
	}
}

func TestCatalogInteractionRoutesAndAcknowledgesInForumThread(t *testing.T) {
	called := false
	session, _ := discordgo.New("Bot test")
	session.Client = &http.Client{Transport: orderTestTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		if !strings.Contains(r.URL.Path, "/interactions/interaction/token/callback") {
			t.Fatalf("wrong acknowledgement path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"type":5`) {
			t.Fatalf("did not defer before database access: %s", body)
		}
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"test rejection","code":50013}`)), Request: r}, nil
	})}
	b := &Bot{guildID: "guild", announces: true, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	b.onInteractionCreate(session, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "interaction", Token: "token", GuildID: "guild", ChannelID: "forum-thread", Type: discordgo.InteractionApplicationCommand, Data: discordgo.ApplicationCommandInteractionData{Name: "catalog"}}})
	if !called {
		t.Fatal("catalog interaction silently ignored")
	}
}

func TestCatalogPostButton(t *testing.T) {
	if options := catalogSlashCommand(nil).Options; len(options) != 1 || options[0].Name != "partner" {
		t.Fatal("public parameter must be removed")
	}
	for _, posted := range []bool{false, true} {
		button := catalogPostButton("123", "SOT", posted)[0].(discordgo.ActionsRow).Components[0].(discordgo.Button)
		if button.Disabled != posted || button.CustomID != "catalog:post:123:SOT" {
			t.Fatal("incorrect button state")
		}
	}
}

func TestInternalCatalog(t *testing.T) {
	entries := []business.Entry{
		{PartnerCode: "SOT", Direction: "internal", Name: "Vector package", Unit: "package", Price: 18500, Notes: "2 Joint: Free", StockGroup: "weapon"},
		{PartnerCode: "SOT", Direction: "internal", Name: "Vector", Unit: "piece", Price: 8500, StockGroup: "weapon"},
		{PartnerCode: "SOT", Direction: "internal", Name: "Ammo SMG", Unit: "clip", Price: 2500, StockGroup: "ammo", Notes: "150 rounds/clip."},
		{PartnerCode: "SOT", Direction: "internal", Name: "Joint", Unit: "piece", Price: 500, StockGroup: "body_drugs"},
		{PartnerCode: "BZ", Direction: "sale", Name: "Hidden", Price: 1},
	}
	edit, err := partnerCatalogResponse(entries, " sot ")
	if err != nil {
		t.Fatal(err)
	}
	if len(*edit.Embeds) != 4 {
		t.Fatalf("expected four groups: %+v", edit.Embeds)
	}
	labels := []string{"Member packages", "À la carte · Weapons", "À la carte · Ammo", "À la carte · Body & Drugs"}
	for index, embed := range *edit.Embeds {
		if embed.Title != "SOT · "+labels[index] || len(embed.Fields) != 1 {
			t.Fatalf("incorrect group: %+v", embed)
		}
	}
	if !strings.Contains((*edit.Embeds)[0].Fields[0].Value, "2 Joint: Free") {
		t.Fatal("package contents missing")
	}
	if !strings.Contains((*edit.Embeds)[2].Fields[0].Value, "150 rounds/clip.") {
		t.Fatal("ammo quantity missing")
	}
	entries[1].Price = 9000
	edit, _ = partnerCatalogResponse(entries, "SOT")
	if !strings.Contains((*edit.Embeds)[1].Fields[0].Value, "$9,000") {
		t.Fatal("database price not reflected")
	}
	entries[0].Notes = strings.Repeat("x", 5000)
	edit, err = partnerCatalogResponse(entries, "SOT")
	if err != nil || len(edit.Files) != 1 {
		t.Fatal("large internal catalog fallback missing")
	}
}

func TestCatalogPostRejectsOtherUser(t *testing.T) {
	called := false
	session := &discordgo.Session{Ratelimiter: discordgo.NewRatelimiter(), Client: &http.Client{Transport: orderTestTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Only the catalog requester") || !strings.Contains(string(body), `"flags":64`) {
			t.Fatalf("unexpected rejection: %s", body)
		}
		return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	b := &Bot{guildID: "guild", announces: true}
	b.onInteractionCreate(session, &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "id", Token: "token", GuildID: "guild", Type: discordgo.InteractionMessageComponent, Member: &discordgo.Member{User: &discordgo.User{ID: "other"}}, Data: discordgo.MessageComponentInteractionData{CustomID: "catalog:post:owner:SOT"}}})
	if !called {
		t.Fatal("catalog post component not routed")
	}
}
