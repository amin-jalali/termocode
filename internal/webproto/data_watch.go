package webproto

import "time"

// The watch socket (MOB-23, mobile plan §0 "Push without our own cloud",
// §3): the Android app's foreground service keeps one long GET /watch
// open over the tailnet and shows local notifications from it. The page
// gets a watch token with app.watch_token (a paired device only, never a
// shared viewer or a launch-link tab) and hands it to the app's service.
// A watch token opens /watch and nothing else: it is not a session, it
// cannot call an op or read a topic. The socket sends only the needs-you
// line, the counts and the items that newly need the user (title, kind
// and tap link, the same the page's Home shows), never file text, a path
// outside the workspace or a secret.
//
// The hub tells the gateway its inbox through the control connection
// while a phone watches: the gateway calls web.watching {on} when its
// first watch socket opens and its last one closes, and the hub sends
// EvInboxWatch events with the whole inbox meanwhile.

// Watch op names.
const (
	OpAppWatchToken = "app.watch_token"
	OpWebWatching   = "web.watching"
)

// EvInboxWatch is the control event with the home hub's inbox (an
// InboxSnap) while a phone watches.
const EvInboxWatch = "inbox_watch"

// Watch limits.
const (
	// WatchTTL is how long a watch token works; the page asks for a new
	// one when it opens and the app keeps the newest.
	WatchTTL = 30 * 24 * time.Hour
	// MaxWatchTokens is how many live watch tokens one device keeps (the
	// oldest goes first): an app reinstall must not pile them up.
	MaxWatchTokens = 2
	// MaxWatchNew caps the new items of one frame.
	MaxWatchNew = 10
	// WatchPing is how often an idle socket gets a ping frame.
	WatchPing = 25 * time.Second
	// WatchEvery is the least time between two frames (a busy inbox
	// coalesces into one frame).
	WatchEvery = time.Second
)

var opWatchRows = []Policy{
	// A viewer may watch too: the socket shows what its page shows.
	{Op: OpAppWatchToken, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 10 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2, Gateway: true},
	{Op: OpWebWatching, New: newOf[WatchingArgs](), Role: RoleCtl, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opWatchRows...); return true }()

func init() { registerResult(OpAppWatchToken, WatchToken{}) }

// WatchToken is the answer of app.watch_token. Token is shown once and
// goes in "Authorization: Bearer <token>" on GET Path (never in the URL:
// URLs end up in logs).
type WatchToken struct {
	Token   string `json:"token"`
	Path    string `json:"path"`    // "/watch"
	Expires int64  `json:"expires"` // Unix ms
}

// WatchingArgs: the gateway's first watch socket opened (On) or its last
// one closed.
type WatchingArgs struct {
	On bool `json:"on"`
}

// WatchFrame is one line of the /watch stream (NDJSON). T is "watch"
// (the state now) or "ping" (nothing else set). The first frame is the
// state at connect; New lists only items that newly need the user since
// the socket's last frame (none in the first frame: the app shows the
// line, not a burst of old items).
type WatchFrame struct {
	T      string      `json:"t" enum:"watch|ping"`
	Line   string      `json:"line,omitempty"` // "1 thing needs you · shop"; "" when nothing does
	Counts InboxCounts `json:"counts"`
	New    []WatchItem `json:"new,omitempty"`
}

// WatchItem is one new needs-you item for a local notification.
type WatchItem struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Link  string `json:"link"` // InboxItemLink: where a tap opens the page
}

// WatchFrameOf is the frame of the merged inbox now, with the items new
// since prev (nil prev: the first frame, no new items). Items of the home
// workspace (ws == home) get a link without ws: the page's own project.
func WatchFrameOf(prev *InboxSnap, now InboxSnap, line, home string) WatchFrame {
	f := WatchFrame{T: "watch", Line: line, Counts: now.Counts}
	if prev == nil {
		return f
	}
	for _, it := range InboxNewNeedsYou(*prev, now) {
		if len(f.New) == MaxWatchNew {
			break
		}
		if it.WS == home {
			it.WS = ""
		}
		f.New = append(f.New, WatchItem{ID: it.ID, Kind: it.Kind, Title: InboxTitle(it.Title), Link: InboxItemLink(it)})
	}
	return f
}
