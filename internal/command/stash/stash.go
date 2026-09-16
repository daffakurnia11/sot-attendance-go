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
	"unicode/utf8"

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

// transactionRows lays a movement out as name, the amount moved, and the
// balance it left behind, each column padded to the widest in the message.
//
// The amount carries its sign rather than a multiplication cross: a line
// quoted on its own said "x 50" whichever way the stock went, and the only
// thing that distinguished a deposit from a withdrawal was the embed's colour.
//
// Widths count runes for the same reason balanceRows does.
func transactionRows(lines []Line, action string) []string {
	sign := "+"
	if action == stockdomain.ActionWithdraw {
		sign = "-"
	}
	nameWidth, movedWidth, afterWidth := 0, 0, 0
	moved, after := make([]string, len(lines)), make([]string, len(lines))
	for index, line := range lines {
		moved[index] = sign + FormatNumber(int64(line.Quantity))
		after[index] = FormatNumber(int64(line.After))
		if width := utf8.RuneCountInString(line.Name); width > nameWidth {
			nameWidth = width
		}
		if width := len(moved[index]); width > movedWidth {
			movedWidth = width
		}
		if width := len(after[index]); width > afterWidth {
			afterWidth = width
		}
	}
	rows := make([]string, 0, len(lines))
	for index, line := range lines {
		padding := strings.Repeat(" ", nameWidth-utf8.RuneCountInString(line.Name))
		rows = append(rows, fmt.Sprintf("%s%s  %*s  →  %*s", line.Name, padding, movedWidth, moved[index], afterWidth, after[index]))
	}
	return rows
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
	rows := BoundedLines(transactionRows(lines, action), 1024-2*len(balanceFence)-2)
	return &discordgo.MessageEmbed{
		Title:       title + " · " + SafeboxLabel(safebox),
		Description: description,
		Color:       color,
		Fields:      []*discordgo.MessageEmbedField{{Name: "Items", Value: balanceFence + "\n" + rows + "\n" + balanceFence}},
		Timestamp:   time.Now().Format(time.RFC3339),
	}
}

// BalanceEmbed lists one safebox, one field per stock group. Items arrive
// already ordered by group, so grouping is a scan rather than a sort.
// balanceFence wraps a group's rows in a fenced block. Discord renders those
// in a monospace face, which is what makes the padding in balanceRows line up;
// the same trick already carries the player log line.
const balanceFence = "```"

// balanceGroup is one heading and the items under it.
type balanceGroup struct {
	group string
	items []Item
}

// balanceRows lays items out as name left, quantity right, padded to widths
// measured across the whole balance rather than one group.
//
// A balance is read for its figures, and a per-group width put every group's
// numbers at a different column, so the eye had to find the edge again at each
// heading. One width for all of them means the figures form a single line down
// the embed.
//
// Widths count runes rather than bytes: an item name is free to carry
// non-ASCII, and counting its bytes would pad it into a crooked column.
//
// ponytail: a rune is assumed one column wide, which holds for the accented
// Latin the catalog uses. A full-width script would still sit crooked; reach
// for a display-width table only if an item is ever named in one.
func balanceRows(items []Item, nameWidth, numberWidth int) []string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		padding := strings.Repeat(" ", nameWidth-utf8.RuneCountInString(item.Name))
		rows = append(rows, fmt.Sprintf("%s%s  %*s", item.Name, padding, numberWidth, FormatNumber(int64(item.Quantity))))
	}
	return rows
}

// balanceWidths measures the widest name and the widest figure across every
// group that will be shown, so a group left out pads nothing.
func balanceWidths(groups []balanceGroup) (nameWidth, numberWidth int) {
	for _, entry := range groups {
		for _, item := range entry.items {
			if width := utf8.RuneCountInString(item.Name); width > nameWidth {
				nameWidth = width
			}
			if width := len(FormatNumber(int64(item.Quantity))); width > numberWidth {
				numberWidth = width
			}
		}
	}
	return nameWidth, numberWidth
}

// stockedGroups splits items into their groups, dropping any whose items all
// read zero.
//
// A group where everything is zero says nothing a reader can act on, and there
// are enough of those to push the groups that do off the screen. The items
// still exist; the group is simply not shown until one of them is stocked.
func stockedGroups(items []Item) []balanceGroup {
	groups := make([]balanceGroup, 0, 8)
	for _, item := range items {
		if len(groups) == 0 || groups[len(groups)-1].group != item.Group {
			groups = append(groups, balanceGroup{group: item.Group})
		}
		current := &groups[len(groups)-1]
		current.items = append(current.items, item)
	}
	stocked := groups[:0]
	for _, entry := range groups {
		for _, item := range entry.items {
			if item.Quantity > 0 {
				stocked = append(stocked, entry)
				break
			}
		}
	}
	return stocked
}

func BalanceEmbed(safebox string, items []Item) *discordgo.MessageEmbed {
	groups := stockedGroups(items)
	nameWidth, numberWidth := balanceWidths(groups)

	// One group per row, full width. Side by side, a nine-item group beside a
	// four-item one left a gap the height of the difference, and the two
	// columns of figures never lined up with each other.
	fields := make([]*discordgo.MessageEmbedField, 0, len(groups))
	for _, entry := range groups {
		// The fences and their newlines come out of the field's own budget.
		rows := BoundedLines(balanceRows(entry.items, nameWidth, numberWidth), 1024-2*len(balanceFence)-2)
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  GroupLabel(entry.group),
			Value: balanceFence + "\n" + rows + "\n" + balanceFence,
		})
	}
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
	case "thief_tools":
		return "Thief Tools"
	case "weapon_accessories":
		return "Weapon Accessories"
	case "electronic_tools":
		return "Electronic Tools"
	case "robbery_item":
		return "Robbery Item"
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
// Both channels are a stock ledger read by people and by the crafting
// calculator alike, so a line of conversation in one is noise in a record. The
// notice names the commands that do record something, because a member who
// wrote the wrong thing needs the right thing in front of them, not a rule.
func ChannelWarning(userDiscordID, safebox string) string {
	return fmt.Sprintf(
		"<@%s> This channel only records **%s** movements. Run `/%s deposit` or `/%s withdraw` and pick the items from the menu. This notice disappears shortly.",
		userDiscordID, SafeboxLabel(safebox), Command, Command,
	)
}
