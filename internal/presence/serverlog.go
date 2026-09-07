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
	// The heading line names the player; the status line names the username.
	PlayerName string
	Username   string
	// DiscordUserID renders the player as a mention beside their name, which
	// Discord resolves to the live handle and a hover card. Empty when the
	// player has no Discord id stored, and ignored unless it is all digits: a
	// malformed mention renders as raw text.
	DiscordUserID string
	// Status is the stored value: connecting, connected, or disconnected.
	Status     string
	OccurredAt time.Time
	// ServerID is the slot the game server assigned. Empty when the event
	// carried none, which is the normal case for connecting: the id at that
	// point is a temporary deferral number, not the slot the player will hold.
	ServerID string
	// Reason is the disconnect reason. Empty for the other two statuses.
	Reason string
	// StartedAt is the first event of the visit, used for the play time a
	// disconnect reports. Zero when unknown, which reads as unavailable.
	StartedAt time.Time
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
//
// The player is named in the body rather than the embed title because the
// title renders no markdown and no mentions - only the whole title can be a
// link. A mention in the description resolves to the live Discord handle and
// opens the profile card on hover, which is what a reader chasing an entry
// wants, and it does not ping.
func ServerLogEmbed(event ServerLogEvent) *discordgo.MessageEmbed {
	phase := serverLogPhase(event.Status)
	description := fmt.Sprintf("```\n%s\n```", serverLogLine(event, phase))
	if heading := serverLogHeading(event); heading != "" {
		description = heading + "\n" + description
	}
	return embed.New("").
		Color(playerPhaseColor(phase)).
		Description(description).
		Footer(serverLogFooter(event, phase), "").
		Build()
}

// serverLogHeading names the player above the reported line, as a bold name
// followed by their Discord account in brackets.
func serverLogHeading(event ServerLogEvent) string {
	name := escapeMarkdown(strings.TrimSpace(event.PlayerName))
	mention := ""
	if isDiscordSnowflake(event.DiscordUserID) {
		mention = fmt.Sprintf("<@%s>", event.DiscordUserID)
	}

	switch {
	case name != "" && mention != "":
		return fmt.Sprintf("**%s** (%s)", name, mention)
	case name != "":
		return fmt.Sprintf("**%s**", name)
	default:
		return mention
	}
}

// isDiscordSnowflake reports whether value can be rendered as a mention. Ids
// reach here from an unvalidated payload field, and anything else would show
// as the literal <@...> text.
func isDiscordSnowflake(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// escapeMarkdown neutralises the formatting characters a FiveM display name may
// contain, which would otherwise leak out of the bold name and reformat the
// line.
func escapeMarkdown(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, character := range value {
		if strings.ContainsRune("*_~`|\\>", character) {
			builder.WriteByte('\\')
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

// serverLogLine is what the code block holds. A disconnect reason goes on its
// own line: reasons run long - FiveM sends whole command dumps - and appending
// one to the sentence pushed the slot and username off the first line.
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
			line += fmt.Sprintf("\nReason: %s", reason)
		}
		return line
	default:
		return fmt.Sprintf("%s is connecting..", username)
	}
}

// serverLogFooter carries the event time and - on a disconnect - how long the
// visit lasted. The player is named in the body, not repeated here.
func serverLogFooter(event ServerLogEvent, phase playerPhase) string {
	parts := make([]string, 0, 2)
	parts = append(parts, event.OccurredAt.Format(serverLogTime))
	if phase == phaseDisconnected {
		parts = append(parts, fmt.Sprintf("Playtime: %s", elapsedPlaytime(event.StartedAt, event.OccurredAt)))
	}
	return strings.Join(parts, " • ")
}

// codeBlockSafe flattens a value for use inside a fenced block. Disconnect
// reasons are unvalidated player- and server-supplied text: FiveM timeout
// reasons arrive as multi-line command dumps, and a literal fence inside one
// would close the block early and let the rest render as markdown.
func codeBlockSafe(value string) string {
	value = strings.ReplaceAll(value, "`", "'")
	return strings.Join(strings.Fields(value), " ")
}
