package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The limits the gateway enforces (ADR 0020); meta tells the page.
const (
	webIdleS = 15 * 60      // idle timeout
	webMaxS  = 12 * 60 * 60 // max lifetime
)

// waitWeb reads the next hub message (nil once the hub stopped). Update
// re-arms it after each web message while the hub runs.
func waitWeb(h *webHub) tea.Cmd {
	return func() tea.Msg {
		select {
		case msg := <-h.ch:
			return msg
		case <-h.done:
			return nil
		}
	}
}

// webHelloMsg: a browser said hello; Update answers with meta.
type webHelloMsg struct {
	hub    *webHub
	conn   *webConn
	resume *webproto.Resume
}

// handleWebMsg routes the hub messages. ok=false: not ours.
func (m *Model) handleWebMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case webCallMsg:
		return m.onWebCall(msg), true
	case webHelloMsg:
		m.onWebHello(msg)
		return waitWeb(msg.hub), true
	}
	return nil, false
}

// onWebHello sends meta: who the browser is, what it may do and how the
// laptop looks.
func (m *Model) onWebHello(msg webHelloMsg) {
	c := msg.conn
	c.send(&webproto.Meta{
		T: webproto.KindMeta, V: webproto.Version,
		Conn: c.id, Role: c.role, Device: c.device,
		IdleS: webIdleS, MaxS: webMaxS,
		Viewers: msg.hub.browsers(),
		UI:      webproto.MetaUI{Theme: m.themeID},
		Caps:    webproto.CapsFor(c.role),
	})
}
