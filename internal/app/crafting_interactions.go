package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	commandcrafting "github.com/daffakurniawan/sot-discord-bot/internal/command/crafting"
	craftingdomain "github.com/daffakurniawan/sot-discord-bot/internal/crafting"
)

const (
	craftAddID       = "craft:add"
	craftRemoveID    = "craft:remove"
	craftClearID     = "craft:clear"
	craftCalculateID = "craft:calculate"
	craftEditID      = "craft:edit"
	craftPostID      = "craft:post"
	craftModalPrefix = "craft:quantity:"
	craftQuantityID  = "craft:quantity"
)

type craftDraft struct {
	Items     []craftingdomain.BatchItem
	ExpiresAt time.Time
}

type craftDraftStore struct {
	mu      sync.Mutex
	drafts  map[string]craftDraft
	ttl     time.Duration
	nowFunc func() time.Time
}

func newCraftDraftStore(ttl time.Duration) *craftDraftStore {
	return &craftDraftStore{drafts: make(map[string]craftDraft), ttl: ttl, nowFunc: time.Now}
}

func (s *craftDraftStore) get(key string) []craftingdomain.BatchItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, exists := s.drafts[key]
	if !exists || !draft.ExpiresAt.After(s.nowFunc()) {
		delete(s.drafts, key)
		return nil
	}
	return append([]craftingdomain.BatchItem(nil), draft.Items...)
}

func (s *craftDraftStore) upsert(key, weaponCode string, quantity int64) error {
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
		draft.Items = nil
	}
	for index := range draft.Items {
		if draft.Items[index].WeaponCode == weaponCode {
			draft.Items[index].Quantity = quantity
			draft.ExpiresAt = now.Add(s.ttl)
			s.drafts[key] = draft
			return nil
		}
	}
	if len(draft.Items) >= craftingdomain.MaxRecipes {
		return fmt.Errorf("crafting draft supports at most %d products", craftingdomain.MaxRecipes)
	}
	draft.Items = append(draft.Items, craftingdomain.BatchItem{WeaponCode: weaponCode, Quantity: quantity})
	draft.ExpiresAt = now.Add(s.ttl)
	s.drafts[key] = draft
	return nil
}

func (s *craftDraftStore) remove(key, weaponCode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, exists := s.drafts[key]
	if !exists {
		return
	}
	for index, item := range draft.Items {
		if item.WeaponCode == weaponCode {
			draft.Items = append(draft.Items[:index], draft.Items[index+1:]...)
			break
		}
	}
	draft.ExpiresAt = s.nowFunc().Add(s.ttl)
	s.drafts[key] = draft
}

func (s *craftDraftStore) clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, key)
}

func (b *Bot) handleCraftSlashStart(session *discordgo.Session, interaction *discordgo.Interaction) {
	if !b.deferCraftResponse(session, interaction, true) {
		return
	}
	userID, err := interactionUserID(interaction)
	if err != nil {
		b.editCraftError(session, interaction, err)
		return
	}
	b.editCraftBuilder(session, interaction, craftDraftKey(interaction.GuildID, userID))
}

func (b *Bot) handleCraftInteraction(session *discordgo.Session, interaction *discordgo.Interaction) {
	userID, err := interactionUserID(interaction)
	if err != nil {
		b.logger.Error("identify crafting interaction user", "error", err)
		return
	}
	key := craftDraftKey(interaction.GuildID, userID)
	if interaction.Type == discordgo.InteractionMessageComponent {
		data := interaction.MessageComponentData()
		if data.CustomID == craftAddID {
			if len(data.Values) != 1 {
				b.respondCraftError(session, interaction, errors.New("craft product selection is invalid"))
				return
			}
			b.respondCraftQuantityModal(session, interaction, data.Values[0])
			return
		}
		if !b.deferCraftResponse(session, interaction, false) {
			return
		}
		switch data.CustomID {
		case craftRemoveID:
			if len(data.Values) != 1 {
				b.editCraftError(session, interaction, errors.New("craft product removal is invalid"))
				return
			}
			b.craftDrafts.remove(key, data.Values[0])
			b.editCraftBuilder(session, interaction, key)
		case craftClearID:
			b.craftDrafts.clear(key)
			b.editCraftBuilder(session, interaction, key)
		case craftCalculateID:
			b.editCraftCalculation(session, interaction, key, userID)
		case craftEditID:
			b.editCraftBuilder(session, interaction, key)
		case craftPostID:
			b.postCraftCalculation(session, interaction, key, userID)
		default:
			b.editCraftError(session, interaction, errors.New("unsupported crafting interaction"))
		}
		return
	}

	data := interaction.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, craftModalPrefix) {
		return
	}
	if !b.deferCraftResponse(session, interaction, false) {
		return
	}
	weaponCode := strings.TrimPrefix(data.CustomID, craftModalPrefix)
	quantityText, err := modalTextValue(data.Components, craftQuantityID)
	if err != nil {
		b.editCraftError(session, interaction, err)
		return
	}
	quantity, err := strconv.ParseInt(strings.TrimSpace(quantityText), 10, 64)
	if err != nil || quantity < 1 || quantity > craftingdomain.MaxQuantity {
		b.editCraftError(session, interaction, fmt.Errorf("quantity must be between 1 and %d", craftingdomain.MaxQuantity))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.crafting.Get(ctx, weaponCode); err != nil {
		b.editCraftError(session, interaction, fmt.Errorf("validate crafting recipe: %w", err))
		return
	}
	if err := b.craftDrafts.upsert(key, weaponCode, quantity); err != nil {
		b.editCraftError(session, interaction, err)
		return
	}
	b.editCraftBuilder(session, interaction, key)
}

func (b *Bot) editCraftBuilder(session *discordgo.Session, interaction *discordgo.Interaction, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recipes, err := b.crafting.List(ctx)
	if err != nil {
		b.editCraftError(session, interaction, fmt.Errorf("list crafting recipes: %w", err))
		return
	}
	draft := b.craftDrafts.get(key)
	embed, components := craftBuilderMessage(recipes, draft)
	embeds := []*discordgo.MessageEmbed{embed}
	empty := ""
	if _, err := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &empty, Embeds: &embeds, Components: &components}); err != nil {
		b.logger.Error("edit crafting builder", "guild_id", interaction.GuildID, "error", err)
	}
}

func (b *Bot) editCraftCalculation(session *discordgo.Session, interaction *discordgo.Interaction, key, userID string) {
	items := b.craftDrafts.get(key)
	if len(items) == 0 {
		b.editCraftError(session, interaction, errors.New("add at least one product before calculating"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := craftingdomain.CalculateBatch(ctx, b.crafting, items)
	if err != nil {
		b.editCraftError(session, interaction, err)
		return
	}
	embeds := []*discordgo.MessageEmbed{commandcrafting.Embed(result)}
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Edit Plan", Style: discordgo.SecondaryButton, CustomID: craftEditID},
		discordgo.Button{Label: "Post Result", Style: discordgo.SuccessButton, CustomID: craftPostID},
		discordgo.Button{Label: "Clear", Style: discordgo.DangerButton, CustomID: craftClearID},
	}}}
	empty := ""
	if _, err := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &empty, Embeds: &embeds, Components: &components}); err != nil {
		b.logger.Error("edit crafting calculation", "guild_id", interaction.GuildID, "user_id", userID, "error", err)
		return
	}
	b.logger.Info("crafting slash command sent", "guild_id", interaction.GuildID, "channel_id", interaction.ChannelID, "user_id", userID, "recipe_count", len(result.Recipes), "weapon_quantity", result.TotalRequestedQuantity)
}

func (b *Bot) postCraftCalculation(session *discordgo.Session, interaction *discordgo.Interaction, key, userID string) {
	items := b.craftDrafts.get(key)
	if len(items) == 0 {
		b.editCraftError(session, interaction, errors.New("crafting draft expired"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := craftingdomain.CalculateBatch(ctx, b.crafting, items)
	if err != nil {
		b.editCraftError(session, interaction, err)
		return
	}
	embed := commandcrafting.Embed(result)
	if _, err := session.ChannelMessageSendEmbed(interaction.ChannelID, embed); err != nil {
		b.editCraftError(session, interaction, fmt.Errorf("post crafting calculation: %w", err))
		return
	}
	embeds := []*discordgo.MessageEmbed{embed}
	components := []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Edit Plan", Style: discordgo.SecondaryButton, CustomID: craftEditID},
		discordgo.Button{Label: "Posted", Style: discordgo.SuccessButton, CustomID: craftPostID, Disabled: true},
		discordgo.Button{Label: "Clear", Style: discordgo.DangerButton, CustomID: craftClearID},
	}}}
	content := "Crafting plan posted to this channel."
	if _, err := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components}); err != nil {
		b.logger.Error("confirm posted crafting calculation", "guild_id", interaction.GuildID, "user_id", userID, "error", err)
		return
	}
	b.logger.Info("crafting calculation posted", "guild_id", interaction.GuildID, "channel_id", interaction.ChannelID, "user_id", userID, "recipe_count", len(result.Recipes), "weapon_quantity", result.TotalRequestedQuantity)
}

func craftBuilderMessage(recipes []craftingdomain.RecipeSummary, draft []craftingdomain.BatchItem) (*discordgo.MessageEmbed, []discordgo.MessageComponent) {
	draft = sortedDraft(draft)
	recipeNames := make(map[string]string, len(recipes))
	options := make([]discordgo.SelectMenuOption, 0, min(len(recipes), 25))
	for _, recipe := range recipes {
		recipeNames[recipe.WeaponCode] = recipe.WeaponName
		if len(options) < 25 {
			options = append(options, discordgo.SelectMenuOption{Label: recipe.WeaponName, Value: recipe.WeaponCode, Description: fmt.Sprintf("%ds per craft", recipe.CraftingTimeSeconds)})
		}
	}
	productLines := make([]string, 0, len(draft))
	removeOptions := make([]discordgo.SelectMenuOption, 0, len(draft))
	for _, item := range draft {
		name := recipeNames[item.WeaponCode]
		if name == "" {
			name = item.WeaponCode
		}
		productLines = append(productLines, fmt.Sprintf("**%s** × %d", name, item.Quantity))
		removeOptions = append(removeOptions, discordgo.SelectMenuOption{Label: name, Value: item.WeaponCode, Description: fmt.Sprintf("Quantity: %d", item.Quantity)})
	}
	description := "Select a weapon, enter quantity, then add more products or calculate totals. Draft expires after 10 minutes."
	if len(productLines) > 0 {
		description += "\n\n**Current Products**\n" + strings.Join(productLines, "\n")
	}
	embed := &discordgo.MessageEmbed{Title: "Crafting Calculator", Description: description, Color: 0xF2B63D}
	components := make([]discordgo.MessageComponent, 0, 3)
	if len(options) > 0 {
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{CustomID: craftAddID, Placeholder: "Add or update a product", Options: options, MaxValues: 1}}})
	}
	if len(removeOptions) > 0 {
		components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.SelectMenu{CustomID: craftRemoveID, Placeholder: "Remove a product", Options: removeOptions, MaxValues: 1}}})
	}
	components = append(components, discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.Button{Label: "Calculate", Style: discordgo.PrimaryButton, CustomID: craftCalculateID, Disabled: len(draft) == 0},
		discordgo.Button{Label: "Clear", Style: discordgo.DangerButton, CustomID: craftClearID, Disabled: len(draft) == 0},
	}})
	return embed, components
}

func (b *Bot) respondCraftQuantityModal(session *discordgo.Session, interaction *discordgo.Interaction, weaponCode string) {
	response := &discordgo.InteractionResponse{Type: discordgo.InteractionResponseModal, Data: &discordgo.InteractionResponseData{
		CustomID: craftModalPrefix + weaponCode,
		Title:    "Set Product Quantity",
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.TextInput{CustomID: craftQuantityID, Label: "Quantity", Style: discordgo.TextInputShort, Placeholder: "30", Required: true, MinLength: 1, MaxLength: 5},
		}}},
	}}
	if err := session.InteractionRespond(interaction, response); err != nil {
		b.logger.Error("show crafting quantity modal", "guild_id", interaction.GuildID, "error", err)
	}
}

func (b *Bot) deferCraftResponse(session *discordgo.Session, interaction *discordgo.Interaction, ephemeral bool) bool {
	responseType := discordgo.InteractionResponseDeferredMessageUpdate
	var data *discordgo.InteractionResponseData
	if interaction.Type == discordgo.InteractionApplicationCommand {
		responseType = discordgo.InteractionResponseDeferredChannelMessageWithSource
		data = &discordgo.InteractionResponseData{}
		if ephemeral {
			data.Flags = discordgo.MessageFlagsEphemeral
		}
	}
	if err := session.InteractionRespond(interaction, &discordgo.InteractionResponse{Type: responseType, Data: data}); err != nil {
		b.logger.Error("defer crafting interaction", "guild_id", interaction.GuildID, "error", err)
		return false
	}
	return true
}

func (b *Bot) respondCraftError(session *discordgo.Session, interaction *discordgo.Interaction, err error) {
	b.logger.Warn("invalid crafting interaction", "guild_id", interaction.GuildID, "error", err)
	if respondErr := session.InteractionRespond(interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseChannelMessageWithSource, Data: &discordgo.InteractionResponseData{Content: "Invalid crafting action. Run `/craft` again.", Flags: discordgo.MessageFlagsEphemeral}}); respondErr != nil {
		b.logger.Error("respond crafting interaction error", "guild_id", interaction.GuildID, "error", respondErr)
	}
}

func (b *Bot) editCraftError(session *discordgo.Session, interaction *discordgo.Interaction, err error) {
	b.logger.Error("handle crafting interaction", "guild_id", interaction.GuildID, "channel_id", interaction.ChannelID, "error", err)
	content := "Could not update crafting plan. Check product and quantity, then try again."
	embeds := []*discordgo.MessageEmbed{}
	components := []discordgo.MessageComponent{}
	if _, editErr := session.InteractionResponseEdit(interaction, &discordgo.WebhookEdit{Content: &content, Embeds: &embeds, Components: &components}); editErr != nil {
		b.logger.Error("edit crafting interaction error", "guild_id", interaction.GuildID, "error", editErr)
	}
}

func isCraftInteraction(interaction *discordgo.Interaction) bool {
	if interaction == nil {
		return false
	}
	if interaction.Type == discordgo.InteractionMessageComponent {
		return strings.HasPrefix(interaction.MessageComponentData().CustomID, "craft:")
	}
	if interaction.Type == discordgo.InteractionModalSubmit {
		return strings.HasPrefix(interaction.ModalSubmitData().CustomID, craftModalPrefix)
	}
	return false
}

func interactionUserID(interaction *discordgo.Interaction) (string, error) {
	if interaction.Member == nil || interaction.Member.User == nil || interaction.Member.User.ID == "" {
		return "", errors.New("guild member is required")
	}
	return interaction.Member.User.ID, nil
}

func craftDraftKey(guildID, userID string) string { return guildID + ":" + userID }

func modalTextValue(components []discordgo.MessageComponent, customID string) (string, error) {
	for _, component := range components {
		row, ok := component.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, child := range row.Components {
			input, ok := child.(*discordgo.TextInput)
			if ok && input.CustomID == customID {
				return input.Value, nil
			}
		}
	}
	return "", errors.New("quantity input is missing")
}

func sortedDraft(items []craftingdomain.BatchItem) []craftingdomain.BatchItem {
	result := append([]craftingdomain.BatchItem(nil), items...)
	sort.Slice(result, func(left, right int) bool { return result[left].WeaponCode < result[right].WeaponCode })
	return result
}
