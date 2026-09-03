// Package serverlog ingests FiveM player lifecycle events from the CR Roleplay
// server.
//
// The wire format is docs/fivem-player-log-webhook-contract-v1.md. That
// document is the source of truth for every limit in this file; a validator
// stricter than the published contract rejects legitimate traffic with a
// non-retryable 422.
//
// Ingestion is append-only and order-independent. Events are stored as they
// arrive and session state is derived at read time, so a retry that lands after
// a later event cannot corrupt anything.
package serverlog

import "time"

// ContractVersion is the only version this build serves. Senders declare it
// in the X-SOT-Contract-Version header.
const ContractVersion = "1.0"

const (
	// MaxBodyBytes caps the request body at 16 KiB.
	MaxBodyBytes = 16 << 10
	// TimestampSkew is how far X-SOT-Timestamp may sit from server time.
	TimestampSkew = 300 * time.Second
)

// Limits mirror contract section 3.
const (
	MaxEventID          = 64
	MaxServerName       = 80
	MaxPlayerName       = 128
	MaxUsername         = 128
	MaxCID              = 64
	MinLicense          = 9
	MaxLicense          = 128
	MinPrefixedID       = 7
	MaxFiveMID          = 64
	MaxSteamHex         = 64
	MaxDisconnectReason = 200
	MaxPing             = 100000
	MaxPlayerServerID   = 65535
)

// Statuses stored in server_logs.status.
const (
	StatusConnecting   = "connecting"
	StatusConnected    = "connected"
	StatusDisconnected = "disconnected"
)

// eventTypeToStatus accepts the bare names the FiveM script sends. The older
// "player."-prefixed spellings stay accepted so a sender mid-migration is not
// broken by the rename.
var eventTypeToStatus = map[string]string{
	"connecting":          StatusConnecting,
	"connected":           StatusConnected,
	"disconnected":        StatusDisconnected,
	"player.connecting":   StatusConnecting,
	"player.connected":    StatusConnected,
	"player.disconnected": StatusDisconnected,
}

// Event is the decoded request body, before validation.
//
// The sender supplies only what the FiveM server already knows. Everything the
// backend needs but the game cannot cheaply provide is derived on this side:
// the idempotency key from the body hash, the session from the license, and the
// session start from the first event of that session. See docs/ for why.
type Event struct {
	Player Player      `json:"player"`
	Event  EventDetail `json:"event"`
}

type EventDetail struct {
	Type      string  `json:"type"`
	Timestamp string  `json:"timestamp"`
	Reason    *string `json:"reason,omitempty"`
}

type Player struct {
	ServerID    int         `json:"server_id"`
	Name        string      `json:"name"`
	Username    string      `json:"username"`
	CID         string      `json:"cid"`
	Identifiers Identifiers `json:"identifiers"`
	Ping        *int        `json:"ping,omitempty"`
}

// Identifiers are all required. A JSON null or a missing key decodes to the
// empty string, which validation rejects.
type Identifiers struct {
	License  string `json:"license"`
	Discord  string `json:"discord"`
	FiveM    string `json:"fivem"`
	SteamHex string `json:"steamhex"`
}

// ValidEvent is an Event that passed validation. SessionID, ConnectedAt and
// ServerKey are absent: the repository resolves those.
type ValidEvent struct {
	// Payload is the exact request body. It is stored for debugging and is the
	// idempotency key: UNIQUE on jsonb compares canonicalised content, so a
	// retry dedupes without the sender tracking an event id.
	Payload    []byte
	Status     string
	OccurredAt time.Time

	// PlayerName, Username and CID are stored on server_members, not per event.
	PlayerName string
	Username   string
	CID        string

	License  string
	Discord  string
	FiveM    string
	SteamHex string
}

// AcceptedResult is what the repository reports back for one delivery.
type AcceptedResult struct {
	SessionID      string
	Duplicate      bool
	MatchedMember  bool
	ServerMemberID int64
	MemberID       *int64
	// IdentityMismatch names the stored identifier fields the event disagreed
	// with. The event is still stored; this only drives a warn log.
	IdentityMismatch []string
}
