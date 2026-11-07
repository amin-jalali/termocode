package app

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The watch socket's feed (MOB-23). While a phone keeps GET /watch open,
// the gateway says web.watching {on: true}; the hub then builds the inbox
// even with no page listening and sends it to the gateway on the control
// connection (EvInboxWatch) whenever it changes, at the inbox topic's
// pace. web.watching {on: false} stops it. The gateway merges it with the
// other projects (InboxMux) and the socket sends only the needs-you line,
// the counts and the new items' titles and links.

func init() {
	webOps[webproto.OpWebWatching] = (*Model).webWatching
}

// webWatching answers the gateway's web.watching.
func (m *Model) webWatching(call *webCall) tea.Cmd {
	args, _ := call.args.(*webproto.WatchingArgs)
	if args == nil || m.web == nil {
		call.fail(webproto.CodeBadRequest, "Bad watch state.")
		return nil
	}
	h := m.web
	h.inbox.watch = args.On
	if args.On {
		// The gateway needs the state now, not at the next change.
		cur := m.webInboxBuild()
		h.inbox.last, h.inbox.checked = &cur, webNow()
		h.sendInboxWatch(cur)
	}
	call.reply(nil)
	return nil
}

// webInboxWatchMax caps the needs-you items sent for the watch socket:
// a control line is at most 64 KiB, and the socket only notifies about
// needs-you items (200 items could pass it).
const webInboxWatchMax = 50

// webInboxForWatch keeps the counts and only the newest needs-you items.
func webInboxForWatch(s webproto.InboxSnap) webproto.InboxSnap {
	out := webproto.InboxSnap{Items: []webproto.InboxItem{}, Counts: s.Counts}
	for _, it := range s.Items {
		if it.State != webproto.InboxNeedsYou {
			continue
		}
		if len(out.Items) == webInboxWatchMax {
			out.More++
			continue
		}
		out.Items = append(out.Items, it)
	}
	return out
}

// sendInboxWatch hands the inbox (needs-you items only) to every gateway
// on the control connection.
func (h *webHub) sendInboxWatch(s webproto.InboxSnap) {
	data, err := json.Marshal(webInboxForWatch(s))
	if err != nil {
		recordError("[web] inbox watch: " + err.Error())
		return
	}
	for _, c := range h.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EvInboxWatch, Data: data})
	}
}
