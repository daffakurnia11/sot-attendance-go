package presence

// A phase is how a player's state reads in a log embed. The vocabulary and the
// colours outlived the Discord activity log they were written for: the server
// log embed renders the same three states, reported by the game server over the
// webhook rather than guessed from rich presence.
type playerPhase string

const (
	phaseConnecting   playerPhase = "Connecting.."
	phaseConnected    playerPhase = "Connected"
	phaseDisconnected playerPhase = "Disconnected"

	colorConnecting   = 0xFEE75C
	colorConnected    = 0x57F287
	colorDisconnected = 0xED4245
)

func playerPhaseColor(phase playerPhase) int {
	switch phase {
	case phaseConnecting:
		return colorConnecting
	case phaseConnected:
		return colorConnected
	default:
		return colorDisconnected
	}
}
