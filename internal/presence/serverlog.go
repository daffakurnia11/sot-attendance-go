package presence

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/daffakurniawan/sot-discord-bot/internal/discord/embed"
)

// sourceDirectServer labels entries the FiveM server reported over the webhook,
// as opposed to [sourceDiscordActivity], which are inferred from Discord rich
// presence. Both feeds land in the same channel, so the footer is the only way
// a reader can tell which signal produced a line - and the two disagreeing is
// exactly what the operator needs to notice.
const sourceDirectServer = "Direct Server"

// ServerLogEvent is one webhook event, flattened for rendering.
//
// It carries plain fields rather than the serverlog type so this package stays
// free of that dependency; the bot does the mapping.
type ServerLogEvent struct {
	// PlayerName is the FiveM display name and Username the passport name.
	// The title pairs them, matching the shape of the Discord Activity title.
	PlayerName string
	Username   string
	// Status is the stored value: connecting, connected, or disconnected.
	Status     string
	OccurredAt time.Time
	// StartedAt is the first event of the visit, used for play time.
	StartedAt time.Time
}

// serverLogPhase maps a stored status onto the phase vocabulary the Discord
// Activity log already uses, so both feeds read identically apart from the
// footer.
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

// ServerLogEmbed renders one webhook event for the player log channel.
func ServerLogEmbed(event ServerLogEvent, playerCount int) *discordgo.MessageEmbed {
	phase := serverLogPhase(event.Status)
	title := fmt.Sprintf("%s (%s)", event.PlayerName, event.Username)
	builder := embed.New(title).Color(playerPhaseColor(phase))

	if phase == phaseDisconnected {
		builder.Field("Exit Time", discordTimestamp(event.OccurredAt), true).
			Field("Status", string(phase), true).
			Field("Play Time", elapsedPlaytime(event.StartedAt, event.OccurredAt), true)
	} else {
		builder.Field("Start Time", availableDiscordTimestamp(event.StartedAt), true).
			Field("Status", string(phase), true)
	}

	footer := fmt.Sprintf("SOT Players: %d • %s", playerCount, sourceDirectServer)
	return builder.Footer(footer, "").Timestamp(event.OccurredAt).Build()
}
