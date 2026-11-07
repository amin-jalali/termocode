package webproto

import "unicode/utf8"

// The inbox topic (MOB-2, mobile plan 3.10): one list of what the phone's
// Home shows first — a card that waits, an AI run that needs the user or
// is going, an agent lane that waits, a workflow run, a task run, the
// branch's CI. Each item names its source by Ref (a card id, a run key, a
// worktree id, a workflow run id, a task label, a branch), never by a
// path; titles are cut at MaxInboxTitle and carry no path outside the
// workspace. Newest first, at most MaxInbox items (More counts the rest);
// Counts cover every item. The topic sends the whole list as a delta on
// every change of a source.

func init() {
	registerTopic("inbox", InboxSnap{}, InboxSnap{})
}

// Inbox limits.
const (
	MaxInbox      = 200
	MaxInboxTitle = 120
)

// Inbox item kinds.
const (
	InboxCard     = "card"
	InboxRun      = "run"
	InboxLane     = "lane"
	InboxWorkflow = "workflow"
	InboxTask     = "task"
	InboxCI       = "ci"
	InboxRace     = "race"
)

// Inbox item states, in the order the phone groups them.
const (
	InboxNeedsYou = "needs_you"
	InboxWorking  = "working"
	InboxFailed   = "failed"
	InboxDone     = "done"
)

// InboxSnap is the inbox topic.
type InboxSnap struct {
	Items  []InboxItem `json:"items"`
	Counts InboxCounts `json:"counts"`
	More   int         `json:"more,omitempty"` // items left out after MaxInbox
}

// InboxCounts counts every item by state.
type InboxCounts struct {
	NeedsYou int `json:"needs_you"`
	Working  int `json:"working"`
	Failed   int `json:"failed"`
	Done     int `json:"done"`
}

// InboxItem is one row of the inbox. ID is "<kind>:<ref>" and unique in
// the list; Ref is the source's own id for the source topic's ops. Lane
// is a worktree id (WorktreeIDLen hex digits), Chat a chat's title.
// Since is Unix ms. Can lists what the web may do now, by the source
// topic's words ("answer", "stop", "pause", "resume", "run", "open").
type InboxItem struct {
	ID    string   `json:"id"`
	WS    string   `json:"ws,omitempty"`
	Kind  string   `json:"kind" enum:"card|run|lane|workflow|task|ci|race"`
	Ref   string   `json:"ref"`
	Title string   `json:"title"`
	Lane  string   `json:"lane,omitempty"`
	Chat  string   `json:"chat,omitempty"`
	Since int64    `json:"since"`
	State string   `json:"state" enum:"needs_you|working|failed|done"`
	Can   []string `json:"can,omitempty"`
}

// InboxTitle cuts s to MaxInboxTitle bytes on a rune edge, with "…" when
// it was longer.
func InboxTitle(s string) string {
	if len(s) <= MaxInboxTitle {
		return s
	}
	const ell = "…"
	cut := MaxInboxTitle - len(ell)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ell
}

// Add counts one item.
func (c *InboxCounts) Add(state string) {
	switch state {
	case InboxNeedsYou:
		c.NeedsYou++
	case InboxWorking:
		c.Working++
	case InboxFailed:
		c.Failed++
	case InboxDone:
		c.Done++
	}
}
