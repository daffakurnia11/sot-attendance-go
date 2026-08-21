package app

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/router"
)

func TestSlashCommandsMirrorPrefixCommands(t *testing.T) {
	t.Parallel()

	commands := slashCommands()
	if len(commands) != 2 {
		t.Fatalf("slashCommands() count = %d, want 2", len(commands))
	}
	wantNames := []string{"check", "recap"}
	prefixRouter := router.NewRouter("!")
	for index, command := range commands {
		if command.Name != wantNames[index] || command.Description == "" {
			t.Errorf("command[%d] = %#v", index, command)
		}
		if command.Contexts == nil || len(*command.Contexts) != 1 || (*command.Contexts)[0] != discordgo.InteractionContextGuild {
			t.Errorf("command %q contexts = %#v", command.Name, command.Contexts)
		}
		if got := prefixRouter.Match("!" + command.Name); got != command.Name {
			t.Errorf("slash command %q has no matching prefix command", command.Name)
		}
	}
	if len(commands[0].Options) != 1 || commands[0].Options[0].Name != "member" || commands[0].Options[0].Type != discordgo.ApplicationCommandOptionUser || commands[0].Options[0].Required {
		t.Errorf("check options = %#v", commands[0].Options)
	}
}

func TestIsSlashCommand(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"check", "recap"} {
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
