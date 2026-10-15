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

func TestCatalogVisibility(t *testing.T) {
	command := catalogSlashCommand(nil)
	if len(command.Options) != 2 || command.Options[1].Name != "public" || command.Options[1].Required || command.Options[1].Type != discordgo.ApplicationCommandOptionBoolean {
		t.Fatal("missing optional public flag")
	}
	for _, tc := range []struct {
		name    string
		options []*discordgo.ApplicationCommandInteractionDataOption
		want    discordgo.MessageFlags
	}{
		{name: "default private", want: discordgo.MessageFlagsEphemeral},
		{name: "explicit private", options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "public", Type: discordgo.ApplicationCommandOptionBoolean, Value: false}}, want: discordgo.MessageFlagsEphemeral},
		{name: "public", options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "partner", Type: discordgo.ApplicationCommandOptionString, Value: "BZ"}, {Name: "public", Type: discordgo.ApplicationCommandOptionBoolean, Value: true}}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := catalogResponseFlags(tc.options); got != tc.want {
				t.Fatalf("flags=%v want %v", got, tc.want)
			}
		})
	}
}
