package money

import (
	"testing"

	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		content string
		want    Request
		valid   bool
	}{
		{content: "!money balance", want: Request{Action: "balance"}, valid: true},
		{content: "!money deposit $1.250.000 hasil order", want: Request{Action: "deposit", Amount: 1250000, Reason: "hasil order"}, valid: true},
		{content: "!money withdraw 22,500 beli blueprint", want: Request{Action: "withdraw", Amount: 22500, Reason: "beli blueprint"}, valid: true},
		{content: "!money deposit 0 reason"},
		{content: "!money withdraw 100"},
		{content: "!money balance extra"},
	}
	for _, test := range tests {
		got, err := Parse(test.content, "!")
		if test.valid && (err != nil || got != test.want) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v", test.content, got, err, test.want)
		}
		if !test.valid && err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", test.content)
		}
	}
}

func TestTransactionEmbedUsesCharacterNameAndAccountFooter(t *testing.T) {
	t.Parallel()
	embed := TransactionEmbed(moneydomain.Transaction{Account: moneydomain.AccountOffice, Action: moneydomain.ActionWithdraw, Amount: 22500, BalanceBefore: 1012500, BalanceAfter: 990000, Reason: "Blueprint"}, "Kenji Nakamura")
	if embed.Title != "Withdraw by Kenji Nakamura" || embed.Fields[0].Name != "Before" || embed.Fields[1].Value != "− $ 22.500" || embed.Fields[2].Name != "After" || len(embed.Fields) != 4 || embed.Footer == nil || embed.Footer.Text != "Office Money" {
		t.Fatalf("unexpected embed: %#v", embed)
	}
}
