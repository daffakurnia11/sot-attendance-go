package money

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
)

const Command = "money"

var ErrInvalidSyntax = errors.New("invalid money command syntax")

type Request struct {
	Action  string
	Account moneydomain.Account
	Amount  int64
	Reason  string
}

func Parse(content, prefix string) (Request, error) {
	parts := strings.Fields(strings.TrimSpace(content))
	if len(parts) < 2 || parts[0] != prefix+Command {
		return Request{}, ErrInvalidSyntax
	}
	request := Request{Action: strings.ToLower(parts[1])}
	if request.Action == "balance" && len(parts) == 2 {
		return request, nil
	}
	if (request.Action != moneydomain.ActionDeposit && request.Action != moneydomain.ActionWithdraw) || len(parts) < 4 {
		return Request{}, ErrInvalidSyntax
	}
	amount, err := ParseAmount(parts[2])
	if err != nil {
		return Request{}, ErrInvalidSyntax
	}
	request.Amount = amount
	request.Reason = strings.TrimSpace(strings.Join(parts[3:], " "))
	if request.Reason == "" || len(request.Reason) > 500 {
		return Request{}, ErrInvalidSyntax
	}
	return request, nil
}

func ParseAmount(value string) (int64, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "$"))
	value = strings.NewReplacer(".", "", ",", "", "_", "").Replace(value)
	if value == "" {
		return 0, ErrInvalidSyntax
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, ErrInvalidSyntax
		}
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil || amount <= 0 {
		return 0, ErrInvalidSyntax
	}
	return amount, nil
}

func Usage(prefix string) string {
	return fmt.Sprintf("Usage:\n`%smoney balance`\n`%smoney deposit 100000 reason`\n`%smoney withdraw 100000 reason`", prefix, prefix, prefix)
}

func BalanceEmbed(account moneydomain.Account, balance int64) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{Title: accountLabel(account) + " Balance", Color: 0xF2B63D, Fields: []*discordgo.MessageEmbedField{{Name: "Current Balance", Value: "$ " + formatNumber(balance)}}, Timestamp: time.Now().Format(time.RFC3339)}
}

func TransactionEmbed(transaction moneydomain.Transaction, characterName string) *discordgo.MessageEmbed {
	actionLabel, changePrefix, color := "Deposit", "+", 0x57F287
	if transaction.Action == moneydomain.ActionWithdraw {
		actionLabel, changePrefix, color = "Withdraw", "−", 0xED4245
	}
	characterName = strings.TrimSpace(characterName)
	if characterName == "" {
		characterName = "Unknown Member"
	}
	return &discordgo.MessageEmbed{Title: actionLabel + " by " + characterName, Color: color, Fields: []*discordgo.MessageEmbedField{
		{Name: "Before", Value: "$ " + formatNumber(transaction.BalanceBefore), Inline: true},
		{Name: "Change", Value: changePrefix + " $ " + formatNumber(transaction.Amount), Inline: true},
		{Name: "After", Value: "$ " + formatNumber(transaction.BalanceAfter), Inline: true},
		{Name: "Reason", Value: transaction.Reason},
	}, Footer: &discordgo.MessageEmbedFooter{Text: accountLabel(transaction.Account)}, Timestamp: time.Now().Format(time.RFC3339)}
}

func accountLabel(account moneydomain.Account) string {
	if account == moneydomain.AccountDirty {
		return "Dirty Money"
	}
	return "Office Money"
}

func formatNumber(value int64) string {
	digits := strconv.FormatInt(value, 10)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "." + digits[index:]
	}
	return digits
}
