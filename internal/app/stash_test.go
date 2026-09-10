package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	commandstash "github.com/daffakurniawan/sot-discord-bot/internal/command/stash"
	stockdomain "github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

func testStashChannels() stashChannels {
	return stashChannels{bossDeposit: "1", bossWithdraw: "2", publicDeposit: "3", publicWithdraw: "4"}
}

func TestStashChannelsTarget(t *testing.T) {
	t.Parallel()
	channels := testStashChannels()
	tests := []struct {
		channelID string
		safebox   string
		action    string
		valid     bool
	}{
		{channelID: "1", safebox: "boss", action: stockdomain.ActionDeposit, valid: true},
		{channelID: "2", safebox: "boss", action: stockdomain.ActionWithdraw, valid: true},
		{channelID: "3", safebox: "public", action: stockdomain.ActionDeposit, valid: true},
		{channelID: "4", safebox: "public", action: stockdomain.ActionWithdraw, valid: true},
		{channelID: "5"},
	}
	for _, test := range tests {
		safebox, action, valid := channels.target(test.channelID)
		if valid != test.valid || safebox != test.safebox || action != test.action {
			t.Errorf("target(%q) = %q, %q, %v; want %q, %q, %v", test.channelID, safebox, action, valid, test.safebox, test.action, test.valid)
		}
		if test.valid {
			if got := channels.channelFor(test.safebox, test.action); got != test.channelID {
				t.Errorf("channelFor(%q, %q) = %q, want %q", test.safebox, test.action, got, test.channelID)
			}
		}
	}
}

func TestStashUserErrorNamesTheFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{name: "wrong channel", err: stashChannelError{channels: testStashChannels()}, contains: "<#3>"},
		{name: "wrong action", err: stashActionError{requested: "withdraw", safebox: "boss", channelID: "2"}, contains: "<#2>"},
		{name: "vanished item", err: stashItemError{itemKey: "coper"}, contains: "`coper`"},
		{name: "not admin", err: errStashAdminRequired, contains: "Administrator"},
		{name: "insufficient", err: stockdomain.ErrInsufficientStock, contains: "Insufficient stock"},
		{name: "overflow", err: stockdomain.ErrQuantityOverflow, contains: "supported quantity"},
		{name: "idempotency", err: stockdomain.ErrIdempotencyConflict, contains: "already recorded"},
		{name: "syntax", err: commandstash.ErrInvalidSyntax, contains: "Invalid stash request"},
	}
	for _, test := range tests {
		content, known := stashUserError(test.err)
		if !known || !strings.Contains(content, test.contains) {
			t.Errorf("%s: stashUserError() = %q, %v; want text containing %q", test.name, content, known, test.contains)
		}
	}
	if content, known := stashUserError(errors.New("database is down")); known {
		t.Errorf("fault reported as user error: %q", content)
	}
}

// An unknown item key must still read as ErrItemNotFound so callers matching
// the repository sentinel keep working.
func TestStashItemErrorUnwrapsToItemNotFound(t *testing.T) {
	t.Parallel()
	if !errors.Is(stashItemError{itemKey: "coper"}, stockdomain.ErrItemNotFound) {
		t.Fatal("stashItemError does not unwrap to ErrItemNotFound")
	}
}

func TestSlashUserErrorCoversStash(t *testing.T) {
	t.Parallel()
	if got := slashUserError(errStashAdminRequired); !strings.Contains(got, "stash stock") {
		t.Fatalf("slashUserError() = %q", got)
	}
}

func testStashItems() []commandstash.Item {
	return []commandstash.Item{
		{Safebox: "public", ItemKey: "copper", Name: "Copper", Group: "crafting", Quantity: 7346},
		{Safebox: "public", ItemKey: "iron", Name: "Iron", Group: "crafting", Quantity: 6074},
		{Safebox: "public", ItemKey: "ammo_smg", Name: "Ammo SMG", Group: "ammo", Quantity: 6150},
	}
}

func TestStashBuilderMessageOffersOnlyStoredItems(t *testing.T) {
	t.Parallel()
	embed, components := stashBuilderMessage("public", stockdomain.ActionDeposit, testStashItems(), stashDraft{})
	if embed.Title != "Deposit · Public Stash" {
		t.Fatalf("unexpected title: %q", embed.Title)
	}
	// Group menu, item menu, buttons. No remove menu while the draft is empty.
	if len(components) != 3 {
		t.Fatalf("component rows = %d, want 3", len(components))
	}
	itemMenu, ok := components[1].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if !ok || itemMenu.CustomID != stashAddID {
		t.Fatalf("unexpected item menu: %#v", components[1])
	}
	// Defaults to the first group, so only its items are offered.
	if len(itemMenu.Options) != 2 || itemMenu.Options[0].Value != "copper" || itemMenu.Options[1].Value != "iron" {
		t.Fatalf("item options = %#v", itemMenu.Options)
	}
	if itemMenu.Options[0].Description != "In stash: 7,346" {
		t.Errorf("option description = %q", itemMenu.Options[0].Description)
	}
	buttons := components[2].(discordgo.ActionsRow).Components
	if submit := buttons[1].(discordgo.Button); submit.CustomID != stashSubmitID || !submit.Disabled {
		t.Errorf("submit button = %#v", submit)
	}
}

func TestStashBuilderMessageWithDraft(t *testing.T) {
	t.Parallel()
	draft := stashDraft{Group: "ammo", Reason: "boss order", Items: []stockdomain.TransactionLine{{ItemKey: "ammo_smg", Quantity: 1000}}}
	embed, components := stashBuilderMessage("boss", stockdomain.ActionWithdraw, testStashItems(), draft)
	if !strings.Contains(embed.Description, "**Ammo SMG** × 1,000") || !strings.Contains(embed.Description, "**Reason** — boss order") {
		t.Fatalf("unexpected description: %q", embed.Description)
	}
	if len(components) != 4 {
		t.Fatalf("component rows = %d, want 4", len(components))
	}
	removeMenu := components[2].(discordgo.ActionsRow).Components[0].(discordgo.SelectMenu)
	if removeMenu.CustomID != stashRemoveID || len(removeMenu.Options) != 1 || removeMenu.Options[0].Value != "ammo_smg" {
		t.Fatalf("remove menu = %#v", removeMenu)
	}
	if submit := components[3].(discordgo.ActionsRow).Components[1].(discordgo.Button); submit.Disabled {
		t.Error("submit disabled with items and a reason set")
	}
}

func TestStashDraftStoreRoundTrip(t *testing.T) {
	t.Parallel()
	store := newStashDraftStore(time.Minute)
	key := stashDraftKey("guild", "channel", "user")
	if err := store.touch(key, func(draft *stashDraft) error {
		draft.Group = "crafting"
		draft.Items = append(draft.Items, stockdomain.TransactionLine{ItemKey: "copper", Quantity: 500})
		return nil
	}); err != nil {
		t.Fatalf("touch() error = %v", err)
	}
	if got := store.get(key); got.Group != "crafting" || len(got.Items) != 1 {
		t.Fatalf("get() = %#v", got)
	}
	// A returned draft is a copy: mutating it must not reach the store.
	got := store.get(key)
	got.Items[0].Quantity = 1
	if store.get(key).Items[0].Quantity != 500 {
		t.Fatal("get() returned the stored slice")
	}
	if err := store.touch(key, func(draft *stashDraft) error { return errors.New("refused") }); err == nil {
		t.Fatal("touch() error = nil")
	}
	store.clear(key)
	if got := store.get(key); len(got.Items) != 0 {
		t.Fatalf("clear() left %#v", got)
	}
}

func TestStashDraftStoreExpires(t *testing.T) {
	t.Parallel()
	store := newStashDraftStore(time.Minute)
	now := time.Now()
	store.nowFunc = func() time.Time { return now }
	key := stashDraftKey("guild", "channel", "user")
	_ = store.touch(key, func(draft *stashDraft) error { draft.Reason = "weekly farm"; return nil })
	now = now.Add(2 * time.Minute)
	if got := store.get(key); got.Reason != "" {
		t.Fatalf("expired draft survived: %#v", got)
	}
}

func TestStashItemErrorReportsAVanishedRow(t *testing.T) {
	t.Parallel()
	content, known := stashUserError(stashItemError{itemKey: "lsd"})
	if !known || !strings.Contains(content, "`lsd` is no longer in this stash") {
		t.Fatalf("stashUserError() = %q, %v", content, known)
	}
}

func TestInStashChannel(t *testing.T) {
	t.Parallel()
	bot := &Bot{stashChannels: testStashChannels()}
	// Every message in a safebox channel is warned: /stash is the only way to
	// move stock, so nothing typed there is meant to be acted on.
	for _, channelID := range []string{"1", "2", "3", "4"} {
		if !bot.inStashChannel(channelID) {
			t.Errorf("inStashChannel(%q) = false", channelID)
		}
	}
	for _, channelID := range []string{"", "5", "99"} {
		if bot.inStashChannel(channelID) {
			t.Errorf("inStashChannel(%q) = true", channelID)
		}
	}
}
