package presence

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
)

// ServerLogEvent is one webhook event, flattened for rendering.
//
// It carries plain fields rather than the serverlog type so this package stays
// free of that dependency; the bot does the mapping.
type ServerLogEvent struct {
	// PlayerName is the FiveM display name and Username the passport name.
	// The title is the player name alone; the status line names the username.
	PlayerName string
	Username   string
	// Status is the stored value: connecting, connected, or disconnected.
	Status     string
	OccurredAt time.Time
	// ServerID is the slot the game server assigned. Empty when the event
	// carried none, which is the normal case for connecting: the id at that
	// point is a temporary deferral number, not the slot the player will hold.
	ServerID string
	// Reason is the disconnect reason. Empty for the other two statuses.
	Reason string
}

// serverLogPhase maps a stored status onto the phase vocabulary the Discord
// Activity log already uses, so both feeds agree on colour.
func serverLogPhase(status string) playerPhase {
	switch status {
	case "connected":
		return phaseConnected
	case "disconnected":
		return phaseDisconnected
	default:
		return phaseConnecting
	}
}

// serverLogTime is the footer format. Footer text is plain: Discord renders no
// markdown and no <t:> timestamp there, so the time is formatted here rather
// than handed over as markup. The embed timestamp is not used either - Discord
// renders that as "Today at 08.03", which loses the date.
//
// OccurredAt is expected in the location the reader thinks in; the caller
// converts.
const serverLogTime = "02 January 2006 at 15:04"

// ServerLogEmbed renders one webhook event for the server log channel.
//
// The line lives in a code block rather than an embed field: the feed is read
// as a scrollback of what the game server reported, and a monospaced sentence
// stays scannable where a Status/Time field grid does not.
func ServerLogEmbed(event ServerLogEvent) *discordgo.MessageEmbed {
	phase := serverLogPhase(event.Status)
	return embed.New(event.PlayerName).
		Color(playerPhaseColor(phase)).
		Description(fmt.Sprintf("```\n%s\n```", serverLogLine(event, phase))).
		Footer(event.OccurredAt.Format(serverLogTime), "").
		Build()
}

// serverLogLine is the sentence inside the code block.
func serverLogLine(event ServerLogEvent, phase playerPhase) string {
	username := codeBlockSafe(event.Username)
	slot := ""
	if event.ServerID != "" {
		slot = fmt.Sprintf("[%s] ", codeBlockSafe(event.ServerID))
	}

	switch phase {
	case phaseConnected:
		return fmt.Sprintf("%s%s is connected.", slot, username)
	case phaseDisconnected:
		line := fmt.Sprintf("%s%s is exiting.", slot, username)
		if reason := codeBlockSafe(event.Reason); reason != "" {
			line += fmt.Sprintf(" Reason: %s", reason)
		}
		return line
	default:
		return fmt.Sprintf("%s is connecting..", username)
	}
}

// codeBlockSafe flattens a value for use inside a fenced block. Disconnect
// reasons are unvalidated player- and server-supplied text: FiveM timeout
// reasons arrive as multi-line command dumps, and a literal fence inside one
// would close the block early and let the rest render as markdown.
func codeBlockSafe(value string) string {
	value = strings.ReplaceAll(value, "`", "'")
	return strings.Join(strings.Fields(value), " ")
}
