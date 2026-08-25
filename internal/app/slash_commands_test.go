package app

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/router"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
)

func TestSlashCommandsMirrorPrefixCommands(t *testing.T) {
	t.Parallel()

	commands := slashCommands()
	if len(commands) != 4 {
		t.Fatalf("slashCommands() count = %d, want 4", len(commands))
	}
	wantNames := []string{"craft", "check", "recap", "money"}
	prefixRouter := router.NewRouter("!")
	for index, command := range commands {
		if command.Name != wantNames[index] || command.Description == "" {
			t.Errorf("command[%d] = %#v", index, command)
		}
		if command.Contexts == nil || len(*command.Contexts) != 1 || (*command.Contexts)[0] != discordgo.InteractionContextGuild {
			t.Errorf("command %q contexts = %#v", command.Name, command.Contexts)
		}
		prefixContent := "!" + command.Name
		if command.Name == "craft" {
			prefixContent += " vector:1"
		}
		if command.Name == "money" {
			prefixContent += " balance"
		}
		if got := prefixRouter.Match(prefixContent); got != command.Name {
			t.Errorf("slash command %q has no matching prefix command", command.Name)
		}
	}
	if len(commands[1].Options) != 1 || commands[1].Options[0].Name != "member" || commands[1].Options[0].Type != discordgo.ApplicationCommandOptionUser || commands[1].Options[0].Required {
		t.Errorf("check options = %#v", commands[1].Options)
	}
	if len(commands[3].Options) != 3 || commands[3].Options[0].Name != "balance" || commands[3].Options[1].Name != "deposit" || commands[3].Options[2].Name != "withdraw" {
		t.Errorf("money options = %#v", commands[3].Options)
	}
	if len(commands[3].Options[0].Options) != 0 || len(commands[3].Options[1].Options) != 2 || commands[3].Options[1].Options[0].Name != "amount" {
		t.Errorf("money account option still present: %#v", commands[3].Options)
	}
}

func TestMoneyChannelID(t *testing.T) {
	t.Parallel()
	bot := &Bot{officeMoneyChannelID: "office-channel", dirtyMoneyChannelID: "dirty-channel"}
	if got := bot.moneyChannelID(moneydomain.AccountOffice); got != "office-channel" {
		t.Errorf("office channel = %q", got)
	}
	if got := bot.moneyChannelID(moneydomain.AccountDirty); got != "dirty-channel" {
		t.Errorf("dirty channel = %q", got)
	}
	if account, ok := bot.moneyAccountForChannel("office-channel"); !ok || account != moneydomain.AccountOffice {
		t.Errorf("office account = %q, %v", account, ok)
	}
	if account, ok := bot.moneyAccountForChannel("other"); ok || account != "" {
		t.Errorf("other account = %q, %v", account, ok)
	}
	if got := slashUserError(moneyChannelError{officeChannelID: "123", dirtyChannelID: "456"}); got != "Money commands can only be used in <#123> or <#456>." {
		t.Errorf("channel error = %q", got)
	}
}

func TestIsSlashCommand(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"craft", "check", "recap", "money"} {
		if !isSlashCommand(name) {
			t.Errorf("isSlashCommand(%q) = false", name)
		}
	}
	if isSlashCommand("unknown") {
		t.Error("unknown command matched")
	}
}

func TestCheckSlashTargetUserID(t *testing.T) {
	t.Parallel()

	interaction := &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommand,
		Data: discordgo.ApplicationCommandInteractionData{Name: "check"},
	}
	if got, err := checkSlashTargetUserID(interaction, "caller"); err != nil || got != "caller" {
		t.Fatalf("fallback target = %q, error = %v", got, err)
	}
	interaction.Data = discordgo.ApplicationCommandInteractionData{
		Name: "check",
		Options: []*discordgo.ApplicationCommandInteractionDataOption{{
			Name: "member", Type: discordgo.ApplicationCommandOptionUser, Value: "target",
		}},
	}
	if got, err := checkSlashTargetUserID(interaction, "caller"); err != nil || got != "target" {
		t.Fatalf("selected target = %q, error = %v", got, err)
	}
}
