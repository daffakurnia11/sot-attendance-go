package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	commandstash "github.com/daffakurniawan/sot-discord-bot/internal/command/stash"
	stockdomain "github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

var errStashAdminRequired = errors.New("stash command requires administrator permission")

// stashChannelError is raised when a stash command runs outside the two
// configured safebox channels. The channel is the only thing naming the
// safebox, so there is nothing to fall back on.
type stashChannelError struct{ channels stashChannels }

func (err stashChannelError) Error() string {
	return "stash command used outside configured stash channels"
}

// errStashDraftExpired is raised when a builder interaction arrives with no
// action behind it. The action comes from the subcommand that opened the
// builder, so a draft that has expired or was never opened has nothing to
// apply and the member has to start again.
var errStashDraftExpired = errors.New("stash draft is no longer active")

// stashItemError names an item key the safebox no longer holds. No member can
// write one - every key comes from a menu - so this only fires when a draft
// outlives the row it was built from.
type stashItemError struct{ itemKey string }

func (err stashItemError) Error() string { return "unknown stash item " + err.itemKey }

func (err stashItemError) Unwrap() error { return stockdomain.ErrItemNotFound }

type stashChannels struct {
	public string
	boss   string
}

func (c stashChannels) mentions() string {
	return "<#" + c.public + "> or <#" + c.boss + ">"
}

// target resolves a channel to the safebox it stands for. The action is not
// read from the channel: both deposits and withdrawals are recorded in the
// safebox's one channel and the subcommand says which is which.
func (c stashChannels) target(channelID string) (string, bool) {
	switch channelID {
	case c.public:
		return "public", true
	case c.boss:
		return "boss", true
	default:
		return "", false
	}
}

// stashResponse runs one stash command and returns the embed to show for it.
//
// Both command forms share this: the prefix command and the slash command
// differ only in how the request was written and in which Discord ID makes the
// idempotency key. That key is the message or interaction ID, so a Discord
// retry of the same event cannot apply the movement twice.
func (b *Bot) stashResponse(ctx context.Context, userID, channelID, idempotencyKey string, request commandstash.Request) (*discordgo.MessageEmbed, error) {
	safebox, valid := b.stashChannels.target(channelID)
	if !valid {
		return nil, stashChannelError{channels: b.stashChannels}
	}
	items, err := b.stock.List(ctx)
	if err != nil {
		return nil, err
	}
	if request.Action == commandstash.BalanceAction {
		return commandstash.BalanceEmbed(safebox, filterSafebox(items, safebox)), nil
	}
	// Every item is re-checked against the safebox before anything moves. The
	// keys came from a menu, but a draft is held for ten minutes and the row
	// behind one could be gone by the time it is submitted.
	catalog := commandstash.NewCatalog(filterSafebox(items, safebox))
	seen := make(map[string]struct{}, len(request.Items))
	for _, line := range request.Items {
		if !catalog.Holds(line.ItemKey) {
			return nil, stashItemError{itemKey: line.ItemKey}
		}
		if _, duplicate := seen[line.ItemKey]; duplicate {
			return nil, commandstash.ErrInvalidSyntax
		}
		seen[line.ItemKey] = struct{}{}
	}
	currentMember, err := b.members.FindByDiscordUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find stash command member: %w", err)
	}
	if !currentMember.IsAdmin {
		return nil, errStashAdminRequired
	}
	if err := b.stock.Transact(ctx, stockdomain.Transaction{
		Safebox:        safebox,
		Action:         request.Action,
		Reason:         request.Reason,
		ActorMemberID:  currentMember.ID,
		Items:          request.Items,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return nil, err
	}
	// Re-read rather than adding the deltas to the quantities read above: the
	// crafting calculator writes the same balances, so a locally computed
	// "after" would report a number that was never in the safebox.
	updated, err := b.stock.List(ctx)
	if err != nil {
		return nil, err
	}
	after := make(map[string]int32, len(updated))
	for _, item := range updated {
		if item.Safebox == safebox {
			after[item.ItemKey] = item.Quantity
		}
	}
	lines := make([]commandstash.Line, 0, len(request.Items))
	for _, line := range request.Items {
		lines = append(lines, commandstash.Line{Name: catalog.Name(line.ItemKey), Quantity: line.Quantity, After: after[line.ItemKey]})
	}
	b.logger.Info("stash transaction applied", "channel_id", channelID, "user_id", userID, "member_id", currentMember.ID, "safebox", safebox, "action", request.Action, "items", len(request.Items))
	return commandstash.TransactionEmbed(safebox, request.Action, lines, currentMember.CharacterName, userID, request.Reason), nil
}

func filterSafebox(items []stockdomain.Item, safebox string) []stockdomain.Item {
	filtered := make([]stockdomain.Item, 0, len(items))
	for _, item := range items {
		if item.Safebox == safebox {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// stashUserError maps the errors a caller can fix to a message naming the fix.
// Anything else is a fault and reaches the generic handler, which logs it.
func stashUserError(err error) (string, bool) {
	var channelError stashChannelError
	var itemError stashItemError
	switch {
	case errors.As(err, &channelError):
		return "Stash commands can only be used in " + channelError.channels.mentions() + ".", true
	case errors.Is(err, errStashDraftExpired):
		return "That stash builder is no longer active. Run `/stash deposit` or `/stash withdraw` to start again.", true
	case errors.As(err, &itemError):
		return "`" + itemError.itemKey + "` is no longer in this stash. Run `/stash balance`, then start again.", true
	case errors.Is(err, errStashAdminRequired):
		return "Administrator permission is required to change stash stock.", true
	case errors.Is(err, stockdomain.ErrInsufficientStock):
		return "Insufficient stock for that withdrawal.", true
	case errors.Is(err, stockdomain.ErrQuantityOverflow):
		return "That deposit would push the safebox past its supported quantity.", true
	case errors.Is(err, stockdomain.ErrIdempotencyConflict):
		return "That request was already recorded with different contents. Send it again as a new message.", true
	case errors.Is(err, commandstash.ErrInvalidSyntax), errors.Is(err, stockdomain.ErrInvalidTransaction):
		return "Invalid stash request. Check the quantities and the reason, then try again.", true
	default:
		return "", false
	}
}

// inStashChannel reports whether a channel is one of the two safebox
// channels. Every message written in one is answered with the warning: stash
// movements are made through /stash alone, so there is no message a member
// could type there that the bot is meant to act on.
func (b *Bot) inStashChannel(channelID string) bool {
	_, valid := b.stashChannels.target(channelID)
	return valid
}

// stashChannelWarningTTL is how long the warning stays before the bot removes
// it. The member's own message is left alone: deleting it would need Manage
// Messages and would destroy what they wrote over a false positive, while a
// notice that cleans itself up costs the channel nothing.
const stashChannelWarningTTL = 15 * time.Second

func (b *Bot) warnStashChannel(session *discordgo.Session, message *discordgo.MessageCreate) {
	safebox, valid := b.stashChannels.target(message.ChannelID)
	if !valid {
		return
	}
	sent, err := session.ChannelMessageSendComplex(message.ChannelID, &discordgo.MessageSend{
		Content:   commandstash.ChannelWarning(message.Author.ID, safebox),
		Reference: message.Reference(),
		// Only the member who wrote the message is pinged. Without this an
		// item name that happens to read as a mention would notify whoever it
		// resolved to.
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{message.Author.ID}},
	})
	if err != nil {
		b.logger.Error("send stash channel warning", "channel_id", message.ChannelID, "user_id", message.Author.ID, "error", err)
		return
	}
	b.logger.Info("stash channel warning sent", "channel_id", message.ChannelID, "user_id", message.Author.ID, "safebox", safebox)
	time.AfterFunc(stashChannelWarningTTL, func() {
		if err := session.ChannelMessageDelete(sent.ChannelID, sent.ID); err != nil {
			b.logger.Warn("delete stash channel warning", "channel_id", sent.ChannelID, "message_id", sent.ID, "error", err)
		}
	})
}
