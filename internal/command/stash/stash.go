// Package stash renders the Discord stash commands that move items in and out
// of the public and boss safeboxes.
//
// There is no prefix form and no text field anywhere in the flow. Items are
// picked from menus the bot fills out of safebox_stock_items, so a member can
// only name an item the database holds - a written name could be a word for
// the right thing that is not the stored thing, and nothing catches that.
//
// A stash command never names its safebox either. Both the safebox and the
// action are read from the channel the command was run in - the same four
// channels the crafting calculator already posts stock embeds to. The action
// is still chosen as a subcommand and checked against the channel, because a
// member who means to withdraw and picks it in a deposit channel should be
// told so rather than have the opposite applied silently.
package stash

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	stockdomain "github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

const (
	Command = "stash"

	// BalanceAction reports the safebox contents and changes nothing.
	BalanceAction = "balance"

	// MaxItems caps one command at the repository's own per-transaction limit.
	MaxItems = 100
)

var ErrInvalidSyntax = errors.New("invalid stash command syntax")

// Item is the stored safebox row this package renders and resolves against.
type Item = stockdomain.Item

// Request is one stash command. Safebox is absent on purpose: the caller
// resolves it from the channel.
type Request struct {
	Action string
	Items  []stockdomain.TransactionLine
	Reason string
}

// Line is one applied movement, carrying the resulting quantity so the embed
// shows where the safebox landed and not only what moved.
type Line struct {
	Name     string
	Quantity int32
	After    int32
}

func SafeboxLabel(safebox string) string {
	if safebox == "boss" {
		return "Boss Stash"
	}
	return "Public Stash"
}

// TransactionEmbed mirrors the crafting calculator's stock embed, because both
// land in the same channel and a reader should not have to tell them apart.
func TransactionEmbed(safebox, action string, lines []Line, actorName, actorDiscordUserID, reason string) *discordgo.MessageEmbed {
	title, color := "Deposit", 0x57F287
	if action == stockdomain.ActionWithdraw {
		title, color = "Withdraw", 0xED4245
	}
	actorName = strings.TrimSpace(actorName)
	if actorName == "" {
		actorName = "Unknown Member"
	}
	description := "Purpose: " + reason + "\nBy **" + actorName + "**"
	if actorDiscordUserID != "" {
		description += " · <@" + actorDiscordUserID + ">"
	}
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, fmt.Sprintf("**%s** × %s → %s", line.Name, FormatNumber(int64(line.Quantity)), FormatNumber(int64(line.After))))
	}
	return &discordgo.MessageEmbed{
		Title:       title + " · " + SafeboxLabel(safebox),
		Description: description,
		Color:       color,
		Fields:      []*discordgo.MessageEmbedField{{Name: "Items", Value: BoundedLines(rendered, 1024)}},
		Timestamp:   time.Now().Format(time.RFC3339),
	}
}

// BalanceEmbed lists one safebox, one field per stock group. Items arrive
// already ordered by group, so grouping is a scan rather than a sort.
func BalanceEmbed(safebox string, items []Item) *discordgo.MessageEmbed {
	fields := make([]*discordgo.MessageEmbedField, 0, 5)
	group, lines := "", make([]string, 0, len(items))
	flush := func() {
		if group == "" || len(lines) == 0 {
			return
		}
		fields = append(fields, &discordgo.MessageEmbedField{Name: GroupLabel(group), Value: BoundedLines(lines, 1024), Inline: true})
	}
	for _, item := range items {
		if item.Group != group {
			flush()
			group, lines = item.Group, make([]string, 0, len(items))
		}
		lines = append(lines, fmt.Sprintf("**%s** — %s", item.Name, FormatNumber(int64(item.Quantity))))
	}
	flush()
	if len(fields) == 0 {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Items", Value: "None"})
	}
	return &discordgo.MessageEmbed{
		Title:     SafeboxLabel(safebox) + " Balance",
		Color:     0xF2B63D,
		Fields:    fields,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func GroupLabel(group string) string {
	switch group {
	case "body_drugs":
		return "Body & Drugs"
	case "ammo":
		return "Ammo"
	case "weapon":
		return "Weapons"
	case "blueprint":
		return "Blueprints"
	case "crafting":
		return "Crafting"
	default:
		return strings.ToUpper(group[:1]) + group[1:]
	}
}

func FormatNumber(value int64) string {
	digits := strconv.FormatInt(value, 10)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}

func BoundedLines(lines []string, limit int) string {
	if len(lines) == 0 {
		return "None"
	}
	var builder strings.Builder
	for index, line := range lines {
		separator := ""
		if builder.Len() > 0 {
			separator = "\n"
		}
		suffix := fmt.Sprintf("\n…and %d more", len(lines)-index)
		if builder.Len()+len(separator)+len(line) > limit {
			if builder.Len()+len(suffix) <= limit {
				builder.WriteString(suffix)
			}
			break
		}
		builder.WriteString(separator)
		builder.WriteString(line)
	}
	return builder.String()
}

// Catalog is the item list as the database defines it.
//
// Nothing in the stash flow accepts written text, so a catalog does not have
// to guess at what a member meant: every item key reaching it came from a menu
// the bot filled out of safebox_stock_items. It exists to name a key for
// display and to re-check, at submit time, that a key picked minutes ago is
// still a row - a draft outlives the menu it was built from.
type Catalog struct {
	keys  []string
	names map[string]string
}

func NewCatalog(items []Item) *Catalog {
	catalog := &Catalog{keys: make([]string, 0, len(items)), names: make(map[string]string, len(items))}
	for _, item := range items {
		if _, exists := catalog.names[item.ItemKey]; exists {
			continue
		}
		catalog.keys = append(catalog.keys, item.ItemKey)
		catalog.names[item.ItemKey] = item.Name
	}
	return catalog
}

func (c *Catalog) Len() int { return len(c.keys) }

// Name returns the display name for a stored key, or the key itself when the
// catalog does not hold it.
func (c *Catalog) Name(itemKey string) string {
	if name, exists := c.names[itemKey]; exists {
		return name
	}
	return itemKey
}

// Holds reports whether the safebox still carries this item.
func (c *Catalog) Holds(itemKey string) bool {
	_, exists := c.names[itemKey]
	return exists
}

// Groups returns the stock groups in the order the repository listed them.
func Groups(items []Item) []string {
	groups := make([]string, 0, 5)
	for _, item := range items {
		if len(groups) == 0 || groups[len(groups)-1] != item.Group {
			if !contains(groups, item.Group) {
				groups = append(groups, item.Group)
			}
		}
	}
	return groups
}

// InGroup returns one group's items, preserving repository order.
func InGroup(items []Item, group string) []Item {
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Group == group {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

// ChannelWarning is the notice posted when someone writes anything but a stash
// command in a safebox channel.
//
// The four channels are a stock ledger read by people and by the crafting
// calculator alike, so a line of conversation in one is noise in a record.
// The notice names the one action the channel accepts and the command that
// performs it, because a member who wrote the wrong thing needs the right
// thing in front of them, not a rule.
func ChannelWarning(userDiscordID, safebox, action string) string {
	return fmt.Sprintf(
		"<@%s> This channel only records **%s %ss**. Run `/%s %s` and pick the items from the menu. This notice disappears shortly.",
		userDiscordID, SafeboxLabel(safebox), action, Command, action,
	)
}
