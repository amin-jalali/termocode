package webproto

import (
	"strings"
	"time"
)

// The phone remote's queue (CMP-46): while the laptop's AI answers in its
// open chat, chat.queue lines a message up instead of busy. The laptop
// sends the first one, as if typed there, when the turn ends (and at once
// while the AI is idle). {remove} takes one back; {now} steers: the turn
// stops and that one goes next, the rest wait after it. Stop (on the
// laptop or chat.stop) drops the queue, so nothing is sent behind the
// user's back. The queue is the open chat's: another chat drops it. The
// chat topic shows it (ChatSnap.Queue, a state delta's Queue).

// OpChatQueue is the queue op.
const OpChatQueue = "chat.queue"

// MaxChatQueue caps the messages that wait.
const MaxChatQueue = 5

// maxChatQueueID caps a queued message's id ("q-<n>").
const maxChatQueueID = 32

// ChatQueueArgs is exactly one of: Text to line up, Remove to take one
// back, Now to steer with one.
type ChatQueueArgs struct {
	Text   string `json:"text,omitempty"`
	Remove string `json:"remove,omitempty"`
	Now    string `json:"now,omitempty"`
}

func (a *ChatQueueArgs) validate(int) error {
	n := 0
	for _, s := range []string{a.Text, a.Remove, a.Now} {
		if s != "" {
			n++
		}
	}
	switch {
	case n != 1:
		return invalid("give one of text, remove, now")
	case a.Text != "" && strings.TrimSpace(a.Text) == "":
		return invalid("empty text")
	case len(a.Text) > MaxChatText || strings.ContainsRune(a.Text, 0):
		return invalid("chat text is %d bytes", len(a.Text))
	case !chatQueueID(a.Remove) || !chatQueueID(a.Now):
		return invalid("bad queue id")
	}
	return nil
}

// chatQueueID: "" or a short id of letters, digits and dashes.
func chatQueueID(s string) bool {
	if len(s) > maxChatQueueID {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// ChatQueued is one waiting message (its text cut for the page).
type ChatQueued struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ChatQueueList is the queue, first to go first: the result of
// chat.queue and a chat state delta's Queue.
type ChatQueueList struct {
	Queue []ChatQueued `json:"queue"`
}

var opChatQueueRows = []Policy{
	{Op: OpChatQueue, New: newOf[ChatQueueArgs](), Role: RoleController, Rate: Rate{N: 2, Per: time.Second}, MaxBytes: 72 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opChatQueueRows...); return true }()

func init() {
	registerResult(OpChatQueue, ChatQueueList{})
}
