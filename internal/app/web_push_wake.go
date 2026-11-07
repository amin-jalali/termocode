package app

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Wakes for phones (MOB-32). The gateway keeps the phones' UnifiedPush
// endpoints; it tells the hub with web.push_watch when at least one
// phone wants wakes. Only then, at the end of each Update, the hub counts
// the cards that wait for the user (not the web's own laptop cards) and,
// when the count went up, sends {"t":"ev","kind":"wake","data":{"count":N}}
// on that control connection. The gateway posts a content-free wake; the
// count never leaves the laptop. Nothing runs while no phone watches.

func init() {
	webOps[webproto.OpWebPushWatch] = func(m *Model, call *webCall) tea.Cmd { return m.webPushWatch(call) }
	webAfterUpdates = append(webAfterUpdates, (*Model).webPushWake)
}

// webPushWakeState is what the wakes keep (Update only).
type webPushWakeState struct {
	on   map[*webConn]bool // control connections that want wakes
	last int               // cards waiting at the last check
}

// due remembers count and says whether it is more than before.
func (s *webPushWakeState) due(count int) bool {
	d := count > s.last
	s.last = count
	return d
}

// wakeFor is the wake event's data when count is due, else nil.
func (s *webPushWakeState) wakeFor(count int) []byte {
	if !s.due(count) {
		return nil
	}
	b, err := json.Marshal(webproto.WakeNote{Count: count})
	if err != nil {
		return nil
	}
	return b
}

// webPushWatch answers the gateway's web.push_watch.
func (m *Model) webPushWatch(call *webCall) tea.Cmd {
	args, _ := call.args.(*webproto.PushWatchArgs)
	h := m.web
	if h == nil || args == nil || call.conn == nil || call.conn.role != webproto.RoleCtl {
		call.fail(webproto.CodeForbidden, "Only the gateway may ask for wakes.")
		return nil
	}
	if args.On {
		if h.wake.on == nil {
			h.wake.on = map[*webConn]bool{}
		}
		if len(h.wake.on) == 0 {
			h.wake.last = m.webWaitingCards() // wake only for what comes from now on
		}
		h.wake.on[call.conn] = true
	} else {
		delete(h.wake.on, call.conn)
	}
	if len(h.wake.on) == 0 {
		h.wake.on, h.wake.last = nil, 0
	}
	call.reply(nil)
	return nil
}

// webPushWake sends a wake event when more cards wait than before.
func (m *Model) webPushWake(tea.Msg) tea.Cmd {
	h := m.web
	if len(h.wake.on) == 0 {
		return nil
	}
	alive := map[*webConn]bool{}
	for _, c := range h.ctlConns() {
		alive[c] = true
	}
	for c := range h.wake.on {
		if !alive[c] || c.ctx.Err() != nil {
			delete(h.wake.on, c)
		}
	}
	if len(h.wake.on) == 0 {
		h.wake.on, h.wake.last = nil, 0
		return nil
	}
	data := h.wake.wakeFor(m.webWaitingCards())
	if data == nil {
		return nil
	}
	for c := range h.wake.on {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EvWake, Data: data})
	}
	return nil
}

// webWaitingCards counts the cards that wait for the user.
func (m *Model) webWaitingCards() int {
	n := 0
	for _, r := range m.webCards(&webCardsPub{}, false) {
		if r.card.Kind != webproto.CardLaptop {
			n++
		}
	}
	return n
}
