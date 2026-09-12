package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	commandstash "github.com/daffakurniawan/sot-discord-bot/internal/command/stash"
	stockdomain "github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

const (
	stashGroupID       = "stash:group"
	stashAddID         = "stash:add"
	stashRemoveID      = "stash:remove"
	stashClearID       = "stash:clear"
	stashReasonID      = "stash:reason"
	stashSubmitID      = "stash:submit"
	stashReasonModalID = "stash:reason:modal"
	stashReasonInputID = "stash:reason:input"
	stashModalPrefix   = "stash:quantity:"
	stashQuantityID    = "stash:quantity"
)

// stashDraft is one member's unsubmitted movement in one channel. The safebox
// is not held here - it belongs to the channel, and the key carries the
// channel - but the action is, because it comes from the subcommand that
// opened the builder and nothing else records it.
type stashDraft struct {
	Action    string
	Group     string
	Reason    string
	Items     []stockdomain.TransactionLine
	ExpiresAt time.Time
}

type stashDraftStore struct {
	mu      sync.Mutex
	drafts  map[string]stashDraft
	ttl     time.Duration
	nowFunc func() time.Time
}

func newStashDraftStore(ttl time.Duration) *stashDraftStore {
	return &stashDraftStore{drafts: make(map[string]stashDraft), ttl: ttl, nowFunc: time.Now}
}

func (s *stashDraftStore) get(key string) stashDraft {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, exists := s.drafts[key]
	if !exists || !draft.ExpiresAt.After(s.nowFunc()) {
		delete(s.drafts, key)
		return stashDraft{}
	}
	draft.Items = append([]stockdomain.TransactionLine(nil), draft.Items...)
	return draft
}

// touch loads the draft, hands it to mutate, and stores the result with a
// fresh expiry. Every write goes through it so no path forgets to extend the
// TTL while a member is still working.
func (s *stashDraftStore) touch(key string, mutate func(*stashDraft) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.nowFunc()
	for draftKey, existing := range s.drafts {
		if !existing.ExpiresAt.After(now) {
			delete(s.drafts, draftKey)
		}
	}
	draft := s.drafts[key]
	if !draft.ExpiresAt.After(now) {
		draft = stashDraft{}
	}
	if err := mutate(&draft); err != nil {
		return err
	}
	draft.ExpiresAt = now.Add(s.ttl)
	s.drafts[key] = draft
	return nil
}

func (s *stashDraftStore) clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, key)
}

func stashDraftKey(guildID, channelID, userID string) string {
	return guildID + ":" + channelID + ":" + userID
}

func isStashInteraction(interaction *discordgo.Interaction) bool {
	if interaction == nil {
		return false
	}
	if interaction.Type == discordgo.InteractionMessageComponent {
		return strings.HasPrefix(interaction.MessageComponentData().CustomID, "stash:")
	}
	if interaction.Type == discordgo.InteractionModalSubmit {
		return strings.HasPrefix(interaction.ModalSubmitData().CustomID, "stash:")
	}
	return false
}

// handleStashSlashStart opens the builder for /stash deposit and /stash
// withdraw. The channel is checked here, before any draft exists, so a member
// in the wrong channel is told immediately instead of after picking items.
func (b *Bot) handleStashSlashStart(session *discordgo.Session, interaction *discordgo.Interaction, action string) {
	if !b.deferStashResponse(session, interaction, true) {
		return
	}
	userID, err := interactionUserID(interaction)
	if err != nil {
		b.editStashError(session, interaction, err)
		return
	}
	safebox, valid := b.stashChannels.target(interaction.ChannelID)
	if !valid {
		b.editStashError(session, interaction, stashChannelError{channels: b.stashChannels})
		return
	}
	key := stashDraftKey(interaction.GuildID, interaction.ChannelID, userID)
	b.stashDrafts.clear(key)
	if err := b.stashDrafts.touch(key, func(draft *stashDraft) error { draft.Action = action; return nil }); err != nil {
		b.editStashError(session, interaction, err)
		return
	}
	b.editStashBuilder(session, interaction, safebox, action, key)
}

func (b *Bot) handleStashInteraction(session *discordgo.Session, interaction *discordgo.Interaction) {
	userID, err := interactionUserID(interaction)
	if err != nil {
		b.logger.Error("identify stash interaction user", "error", err)
		return
	}
	safebox, valid := b.stashChannels.target(interaction.ChannelID)
	if !valid {
		b.respondStashError(session, interaction, stashChannelError{channels: b.stashChannels})
		return
	}
	key := stashDraftKey(interaction.GuildID, interaction.ChannelID, userID)
	// The action lives in the draft, so a builder whose draft has expired has
	// nothing left to apply and its buttons must not fall through to a default.
	action := b.stashDrafts.get(key).Action
	if action == "" {
		b.respondStashError(session, interaction, errStashDraftExpired)
		return
	}

	if interaction.Type == discordgo.InteractionMessageComponent {
		data := interaction.MessageComponentData()
		// Both modals have to be opened as the first response to the click, so
		// neither may be deferred.
		switch data.CustomID {
		case stashAddID:
			if len(data.Values) != 1 {
				b.respondStashError(session, interaction, errors.New("stash item selection is invalid"))
				return
			}
			b.respondStashQuantityModal(session, interaction, data.Values[0])
			return
		case stashReasonID:
			b.respondStashReasonModal(session, interaction, b.stashDrafts.get(key).Reason)
			return
		}
		if !b.deferStashResponse(session, interaction, false) {
			return
		}
		switch data.CustomID {
		case stashGroupID:
			if len(data.Values) != 1 {
				b.editStashError(session, interaction, errors.New("stash group selection is invalid"))
				return
			}
			group := data.Values[0]
			_ = b.stashDrafts.touch(key, func(draft *stashDraft) error { draft.Group = group; return nil })
			b.editStashBuilder(session, interaction, safebox, action, key)
		case stashRemoveID:
			if len(data.Values) != 1 {
				b.editStashError(session, interaction, errors.New("stash item removal is invalid"))
				return
			}
			itemKey := data.Values[0]
			_ = b.stashDrafts.touch(key, func(draft *stashDraft) error {
				for index, line := range draft.Items {
					if line.ItemKey == itemKey {
						draft.Items = append(draft.Items[:index], draft.Items[index+1:]...)
						break
					}
				}
				return nil
			})
			b.editStashBuilder(session, interaction, safebox, action, key)
		case stashClearID:
			b.stashDrafts.clear(key)
			b.editStashBuilder(session, interaction, safebox, action, key)
		case stashSubmitID:
			b.submitStashDraft(session, interaction, safebox, action, key, userID)
		default:
			b.editStashError(session, interaction, errors.New("unsupported stash interaction"))
		}
		return
	}

	data := interaction.ModalSubmitData()
	if !b.deferStashResponse(session, interaction, false) {
		return
	}
	switch {
	case data.CustomID == stashReasonModalID:
		reason, err := modalTextValue(data.Components, stashReasonInputID)
		if err != nil {
			b.editStashError(session, interaction, err)
			return
		}
		reason = strings.TrimSpace(reason)
		if reason == "" || len(reason) > 500 {
			b.editStashError(session, interaction, errors.New("reason must be between 1 and 500 characters"))
			return
		}
		_ = b.stashDrafts.touch(key, func(draft *stashDraft) error { draft.Reason = reason; return nil })
		b.editStashBuilder(session, interaction, safebox, action, key)
	case strings.HasPrefix(data.CustomID, stashModalPrefix):
		itemKey := strings.TrimPrefix(data.CustomID, stashModalPrefix)
		quantityText, err := modalTextValue(data.Components, stashQuantityID)
		if err != nil {
			b.editStashError(session, interaction, err)
			return
		}
		quantity, err := strconv.ParseInt(strings.TrimSpace(quantityText), 10, 32)
		if err != nil || quantity < 1 || quantity > stockdomain.MaxLineQuantity {
			b.editStashError(session, interaction, fmt.Errorf("quantity must be between 1 and %d", stockdomain.MaxLineQuantity))
			return
		}
		// The item key came from a select menu the bot built out of
		// safebox_stock_items, so it is a stored row by construction. It is
		// checked again on submit anyway, because the draft outlives the menu.
		if err := b.stashDrafts.touch(key, func(draft *stashDraft) error {
			for index := range draft.Items {
				if draft.Items[index].ItemKey == itemKey {
					draft.Items[index].Quantity = int32(quantity)
					return nil
				}
			}
			if len(draft.Items) >= commandstash.MaxItems {
				return fmt.Errorf("a stash movement supports at most %d items", commandstash.MaxItems)
			}
			draft.Items = append(draft.Items, stockdomain.TransactionLine{ItemKey: itemKey, Quantity: int32(quantity)})
			return nil
		}); err != nil {
			b.editStashError(session, interaction, err)
			return
		}
		b.editStashBuilder(session, interaction, safebox, action, key)
	}
}

func (b *Bot) submitStashDraft(session *discordgo.Session, interaction *discordgo.Interaction, safebox, action, key, userID string) {
	draft := b.stashDrafts.get(key)
	if len(draft.Items) == 0 || strings.TrimSpace(draft.Reason) == "" {
		b.editStashError(session, interaction, errors.New("add at least one item and a reason before submitting"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	embed, err := b.stashResponse(ctx, userID, interaction.ChannelID, "discord-interaction:"+interaction.ID, commandstash.Request{Action: action, Items: draft.Items, Reason: draft.Reason})
	if err != nil {
		b.editStashError(session, interaction, err)
		return
	}
	if _, err := session.ChannelMessageSendEmbed(interaction.ChannelID, embed); err != nil {
		// The stock already moved. Say so in the ephemeral reply rather than
		// reporting a failure that would invite the member to submit again.
		b.logger.Error("post stash movement", "channel_id", interaction.ChannelID, "user_id", userID, "error", err)
	}
	b.stashDrafts.clear(key)
	content := commandstash.SafeboxLabel(safebox) + " updated and posted to this channel."
	embeds := []*discordgo.MessageEmbed{embed}
	components := []discordgo.MessageComponent{}
	if _, err := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components}); err != nil {
		b.logger.Error("confirm stash movement", "channel_id", interaction.ChannelID, "user_id", userID, "error", err)
	}
}

func (b *Bot) editStashBuilder(session *discordgo.Session, interaction *discordgo.Interaction, safebox, action, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := b.stock.List(ctx)
	if err != nil {
		b.editStashError(session, interaction, fmt.Errorf("list safebox stock: %w", err))
		return
	}
	embed, components := stashBuilderMessage(safebox, action, filterSafebox(items, safebox), b.stashDrafts.get(key))
	embeds := []*discordgo.MessageEmbed{embed}
	empty := ""
	if _, err := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &empty, Embeds: &embeds, Components: &components}); err != nil {
		b.logger.Error("edit stash builder", "channel_id", interaction.ChannelID, "error", err)
	}
}

// stashBuilderMessage renders the picker. Every option comes from
// safebox_stock_items, so a member never types an item name and cannot invent
// one. The group menu exists because Discord caps a select at 25 options and
// the catalog is larger than that.
func stashBuilderMessage(safebox, action string, items []commandstash.Item, draft stashDraft) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	catalog := commandstash.NewCatalog(items)
	groups := commandstash.Groups(items)
	group := draft.Group
	if group == "" && len(groups) > 0 {
		group = groups[0]
	}

	title, color := "Deposit", 0x57F287
	if action == stockdomain.ActionWithdraw {
		title, color = "Withdraw", 0xED4245
	}
	description := "Pick a group, pick an item, then enter its quantity. Every item comes from the stash itself, so there is nothing to spell. The draft expires after 10 minutes."
	lines := make([]string, 0, len(draft.Items))
	for _, line := range draft.Items {
		lines = append(lines, fmt.Sprintf("**%s** × %s", catalog.Name(line.ItemKey), commandstash.FormatNumber(int64(line.Quantity))))
	}
	if len(lines) > 0 {
		description += "\n\n**Draft**\n" + commandstash.BoundedLines(lines, 1024)
	}
	reason := strings.TrimSpace(draft.Reason)
	if reason == "" {
		description += "\n\n**Reason** — not set"
	} else {
		description += "\n\n**Reason** — " + reason
	}
	embed := &discordgo.MessageEmbed{Title: title + " · " + commandstash.SafeboxLabel(safebox), Description: description, Color: color}

	components := make([]discordgo.MessageComponent, 0, 4)
	if len(groups) > 0 {
		options := make([]discordgo.SelectMenuOption, 0, len(groups))
		for _, name := range groups {
			options = append(options, discordgo.SelectMenuOption{Label: commandstash.GroupLabel(name), Value: name, Default: name == group})
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{CustomID: stashGroupID, Placeholder: "Choose a group", Options: options, MaxValues: 1},
		}})
	}
	grouped := commandstash.InGroup(items, group)
	if len(grouped) > 0 {
		options := make([]discordgo.SelectMenuOption, 0, min(len(grouped), 25))
		for _, item := range grouped {
			if len(options) == 25 {
				break
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:       item.Name,
				Value:       item.ItemKey,
				Description: "In stash: " + commandstash.FormatNumber(int64(item.Quantity)),
			})
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{CustomID: stashAddID, Placeholder: "Add or update an item", Options: options, MaxValues: 1},
		}})
	}
	if len(draft.Items) > 0 {
		options := make([]discordgo.SelectMenuOption, 0, min(len(draft.Items), 25))
		for _, line := range draft.Items {
			if len(options) == 25 {
				break
			}
			options = append(options, discordgo.SelectMenuOption{
				Label:       catalog.Name(line.ItemKey),
				Value:       line.ItemKey,
				Description: "Quantity: " + commandstash.FormatNumber(int64(line.Quantity)),
			})
		}
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{CustomID: stashRemoveID, Placeholder: "Remove an item", Options: options, MaxValues: 1},
		}})
	}
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Set Reason", Style: discordgo.SecondaryButton, CustomID: stashReasonID},
		discordgo.Button{Label: "Submit", Style: discordgo.SuccessButton, CustomID: stashSubmitID, Disabled: len(draft.Items) == 0 || reason == ""},
		discordgo.Button{Label: "Clear", Style: discordgo.DangerButton, CustomID: stashClearID, Disabled: len(draft.Items) == 0 && reason == ""},
	}})
	return embed, components
}

func (b *Bot) respondStashQuantityModal(session *discordgo.Session, interaction *discordgo.Interaction, itemKey string) {
	response := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: &discordgo.InteractionResponseData{
		CustomID: stashModalPrefix + itemKey,
		Title:    "Set Item Quantity",
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.TextInput{CustomID: stashQuantityID, Label: "Quantity", Style: discordgo.TextInputShort, Placeholder: "500", Required: true, MinLength: 1, MaxLength: 5},
		}}},
	}}
	if err := session.InteractionRespond(interaction, response); err != nil {
		b.logger.Error("show stash quantity modal", "channel_id", interaction.ChannelID, "error", err)
	}
}

func (b *Bot) respondStashReasonModal(session *discordgo.Session, interaction *discordgo.Interaction, current string) {
	response := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: &discordgo.InteractionResponseData{
		CustomID: stashReasonModalID,
		Title:    "Set Movement Reason",
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.TextInput{CustomID: stashReasonInputID, Label: "Reason", Style: discordgo.TextInputParagraph, Placeholder: "weekly farm", Value: current, Required: true, MinLength: 1, MaxLength: 500},
		}}},
	}}
	if err := session.InteractionRespond(interaction, response); err != nil {
		b.logger.Error("show stash reason modal", "channel_id", interaction.ChannelID, "error", err)
	}
}

func (b *Bot) deferStashResponse(session *discordgo.Session, interaction *discordgo.Interaction, ephemeral bool) bool {
	responseType := discordgo.InteractionResponseDeferredMessageUpdate
	var data *discordgo.InteractionResponseData
	// A modal submit defers as a message update, not as a new message: the
	// modal was opened from the ephemeral builder, and the reply has to edit
	// that builder rather than post a second, public one.
	if interaction.Type == discordgo.InteractionApplicationCommand {
		responseType = discordgo.InteractionResponseDeferredChannelMessageWithSource
		data = &discordgo.InteractionResponseData{}
		if ephemeral {
			data.Flags = discordgo.MessageFlagsEphemeral
		}
	}
	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{Type: responseType, Data: data}); err != nil {
		b.logger.Error("defer stash interaction", "channel_id", interaction.ChannelID, "error", err)
		return false
	}
	return true
}

func (b *Bot) respondStashError(session *discordgo.Session, interaction *discordgo.Interaction, err error) {
	content, known := stashUserError(err)
	if !known {
		b.logger.Warn("invalid stash interaction", "channel_id", interaction.ChannelID, "error", err)
		content = "Invalid stash action. Run `/stash` again."
	}
	if respondErr := session.InteractionRespond(interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: content, Flags: discordgo.MessageFlagsEphemeral}}); respondErr != nil {
		b.logger.Error("respond stash interaction error", "channel_id", interaction.ChannelID, "error", respondErr)
	}
}

func (b *Bot) editStashError(session *discordgo.Session, interaction *discordgo.Interaction, err error) {
	content, known := stashUserError(err)
	if !known {
		b.logger.Error("handle stash interaction", "channel_id", interaction.ChannelID, "error", err)
		content = "Could not update the stash. Try again shortly."
	}
	embeds := []*discordgo.MessageEmbed{}
	components := []discordgo.MessageComponent{}
	if _, editErr := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components}); editErr != nil {
		b.logger.Error("edit stash interaction error", "channel_id", interaction.ChannelID, "error", editErr)
	}
}
