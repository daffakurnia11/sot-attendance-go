package app

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
)

// announcementMergeWindow is how far apart two witnesses' reports of the same
// change may be and still be announced as one message. It matches the window
// migration 000040 snaps visit edges within, so the channel and the playtime
// agree on what counts as the same connect or exit.
const announcementMergeWindow = 10 * time.Minute

// sourceTrust orders the witnesses: the webhook is what the game server itself
// reported, the CFX roster is the server's public list, and Discord activity
// is only what the member's client says it is doing.
func sourceTrust(source string) int {
	switch source {
	case serverlog.SourceCFX:
		return 1
	case serverlog.SourceDiscord:
		return 2
	default:
		return 0
	}
}

type announcementAction int

const (
	announceSkip announcementAction = iota
	announcePost
	announceEdit
)

// postedAnnouncement is a message already in the channel that a later witness
// may still add itself to.
type postedAnnouncement struct {
	messageID  string
	event      presence.ServerLogEvent
	sources    []string
	occurredAt time.Time
}

// announcementMerger folds the three witnesses' reports of one change into one
// channel message.
//
// Each witness writes its own rows, so a visit all three saw produced three
// connects and three exits in the channel. The first report of a change posts;
// any other witness reporting the same change for the same character within
// the window edits that message to add itself, and the most trusted witness's
// details - the slot, the exit reason, the playtime - are the ones shown.
//
// It lives in memory and is owned by the announcer goroutine. A restart
// forgets the posted messages, which costs at most one duplicate per change
// in flight.
type announcementMerger struct {
	posted map[string]*postedAnnouncement
}

func announcementKey(announcement serverlog.Announcement) string {
	return fmt.Sprintf("%d:%s", announcement.ServerMemberID, announcement.Status)
}

// plan decides what one stored row does to the channel: nothing, a new
// message, or an edit of the message returned. event is the row rendered on
// its own; the returned event is what to send.
func (m *announcementMerger) plan(announcement serverlog.Announcement, event presence.ServerLogEvent) (announcementAction, presence.ServerLogEvent, string) {
	if m.posted == nil {
		m.posted = make(map[string]*postedAnnouncement)
	}
	if previous := m.posted[announcementKey(announcement)]; previous != nil &&
		announcement.OccurredAt.Sub(previous.occurredAt).Abs() <= announcementMergeWindow &&
		!slices.Contains(previous.sources, announcement.Source) {
		sources := append(append([]string(nil), previous.sources...), announcement.Source)
		sort.SliceStable(sources, func(i, j int) bool { return sourceTrust(sources[i]) < sourceTrust(sources[j]) })
		merged := previous.event
		if sourceTrust(announcement.Source) < sourceTrust(previous.event.Source) {
			merged = event
		}
		merged.Sources = sources
		return announceEdit, merged, previous.messageID
	}

	// A weaker witness contradicting a stronger one is not news. Its exit
	// while the webhook still has the player on the server is Discord
	// activity turned off or a member gone invisible, not a departure; its
	// connect long after the webhook's is activity switched back on mid-game.
	if since := announcement.TrustedOpenSince; since != nil {
		if announcement.Status == serverlog.StatusDisconnected || announcement.OccurredAt.Sub(*since) > announcementMergeWindow {
			return announceSkip, event, ""
		}
	}
	return announcePost, event, ""
}

// record remembers a message the announcer just sent or edited, so a later
// witness can join it. plan always runs first and creates the map.
func (m *announcementMerger) record(announcement serverlog.Announcement, event presence.ServerLogEvent, messageID string) {
	sources := event.Sources
	if len(sources) == 0 {
		sources = []string{event.Source}
	}
	m.posted[announcementKey(announcement)] = &postedAnnouncement{
		messageID:  messageID,
		event:      event,
		sources:    sources,
		occurredAt: announcement.OccurredAt,
	}
}

// prune forgets messages too old for any witness to still join.
func (m *announcementMerger) prune(now time.Time) {
	for key, posted := range m.posted {
		if now.Sub(posted.occurredAt) > 2*announcementMergeWindow {
			delete(m.posted, key)
		}
	}
}
