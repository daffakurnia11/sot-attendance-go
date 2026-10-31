package app

import (
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/business"
)

func TestBusinessSlashCommandPartnerHintFromCatalog(t *testing.T) {
	t.Parallel()

	entries := []business.Entry{
		{PartnerCode: "BZ", Direction: "purchase"}, {PartnerCode: "BZ", Direction: "sale"}, {PartnerCode: "BZ", Direction: "sale"},
		{PartnerCode: "CN", Direction: "sale"},
		{PartnerCode: "NEW", Direction: "purchase"}, {PartnerCode: "NEW", Direction: "sale"},
		{PartnerCode: "SOT", Direction: "internal"}, {PartnerCode: "SOT", Direction: "sale"},
	}
	partners := partnerCodesByDirection(entries)
	contexts := []discordgo.InteractionContextType{discordgo.InteractionContextGuild}

	if got := businessSlashCommand("order", "d", contexts, partners).Options[0].Description; got != "Partner code: BZ, CN, NEW" {
		t.Errorf("order hint = %q", got)
	}
	if got := businessSlashCommand("purchase", "d", contexts, partners).Options[0].Description; got != "Partner code: BZ, NEW" {
		t.Errorf("purchase hint = %q", got)
	}
	if got := businessSlashCommand("order", "d", contexts, nil).Options[0].Description; got != "Partner code" {
		t.Errorf("empty hint = %q", got)
	}
	long := map[string][]string{"sale": strings.Fields(strings.Repeat("PARTNER ", 30))}
	if got := businessSlashCommand("order", "d", contexts, long).Options[0].Description; len(got) != 100 || !strings.HasSuffix(got, "...") {
		t.Errorf("long hint = %q (%d)", got, len(got))
	}
}

func TestOrderEmbedPartnerShowsFullName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ code, name, want string }{
		{"GWF", "Gang Wolf Family", "Gang Wolf Family (GWF)"},
		{"GWF", "", "GWF"},
		{"SOT", "SOT", "SOT"},
	} {
		embed := orderEmbed(&orderDraft{Direction: "sale", Partner: tc.code, PartnerName: tc.name})
		if got := embed.Fields[0].Value; embed.Fields[0].Name != "Partner" || got != tc.want {
			t.Errorf("partner field for %q/%q = %q, want %q", tc.code, tc.name, got, tc.want)
		}
	}
}
