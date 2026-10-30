package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Laptop cards (web design 3.2, 3.9): a web request that cannot be undone
// and is easy to tap by mistake (discard changes, close a shell) does not
// run when the phone asks. It becomes a Risky card on the laptop, Cancel
// first and focused, naming the device, and a card of kind "laptop" in
// the cards topic, so every browser sees it wait. Only the laptop can
// confirm; the web may cancel. The request answers at once with the
// card's id; the outcome reaches the browsers as the answered event with
// its choice. A dialog already open on the laptop is never replaced: the
// card waits in line and shows when the laptop is free.

// confirmKindWebAsk is the laptop's dialog of a laptop card.
const confirmKindWebAsk confirmKindEnum = 2610

// webMaxAsks caps the laptop cards waiting at once.
const webMaxAsks = 4

func init() {
	// Registered before the cards publisher (file order), so a card that
	// opened, closed or moved in this Update is listed right.
	webAfterUpdates = append(webAfterUpdates, (*Model).webAskPump)
}

// webAsk is one request waiting for the laptop.
type webAsk struct {
	op      string // the op, for the audit line
	conn    string // the asking connection, for the gateway's audit
	summary string // what it acts on (paths), for the audit line
	device  string // who asked
	title   string // "Discard changes?"
	body    string // the laptop card's text, which names the device
	action  string // the action button's label ("Discard")
	run     func(m *Model) tea.Cmd
}

// webAskState is the line of laptop cards (Update only).
type webAskState struct {
	line  []*webAsk // oldest first
	shown *webAsk   // the one whose dialog is open on the laptop
}

// webAskOpen puts a laptop card in line for call and answers with its id.
func (m *Model) webAskOpen(call *webCall, a *webAsk) {
	h := m.web
	if len(h.asks.line) >= webMaxAsks {
		call.fail(webproto.CodeBusy, "Too many questions wait for the laptop.")
		return
	}
	a.op, a.conn, a.device = call.op, call.conn.id, webName(call.conn.device)
	if a.device == "" {
		a.device = "web"
	}
	a.body += "\n\nRequested from " + a.device + "."
	h.asks.line = append(h.asks.line, a)
	id, _ := h.cards.id(a)
	m.webAskShow()
	call.reply(webproto.AskRef{Card: id})
}

// webAskShow opens the first waiting card's dialog when the laptop shows
// no dialog.
func (m *Model) webAskShow() {
	s := &m.web.asks
	if s.shown != nil || m.confirmOpen || len(s.line) == 0 {
		return
	}
	a := s.line[0]
	s.shown = a
	m.confirm = confirm.Risky(a.title, a.body,
		confirm.Button{ID: webproto.ChoiceCancel, Title: "Cancel"},
		confirm.Button{ID: webproto.ChoiceConfirm, Title: a.action, Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindWebAsk
}

// webAskPump runs after every Update: a card whose dialog another dialog
// replaced goes back in line, and the next card shows when the laptop is
// free.
func (m *Model) webAskPump(tea.Msg) tea.Cmd {
	s := &m.web.asks
	if s.shown != nil && !(m.confirmOpen && m.confirmKind == confirmKindWebAsk) {
		s.shown = nil
	}
	m.webAskShow()
	return nil
}

// answerWebAsk is the laptop's answer to the open card's dialog; "" (Esc)
// cancels.
func (m *Model) answerWebAsk(id string) tea.Cmd {
	if m.web == nil || m.web.asks.shown == nil {
		var t tea.Cmd
		m.toast, t = m.toast.Push(toast.Warn, "The web session ended. Nothing was done.")
		return t
	}
	a := m.web.asks.shown
	if id == webproto.ChoiceConfirm {
		return m.webAskDone(a, webproto.ChoiceConfirm, "laptop")
	}
	return m.webAskDone(a, webproto.ChoiceCancel, "laptop")
}

// webAskDone takes a card out of line with its answer: confirm runs the
// action; the audit line says who decided.
func (m *Model) webAskDone(a *webAsk, choice, by string) tea.Cmd {
	h := m.web
	s := &h.asks
	for i, x := range s.line {
		if x == a {
			s.line = append(s.line[:i], s.line[i+1:]...)
			break
		}
	}
	if s.shown == a {
		s.shown = nil
		if m.confirmOpen && m.confirmKind == confirmKindWebAsk {
			m.confirmOpen = false
		}
	}
	id, _ := h.cards.id(a)
	h.cards.decided(id, choice, by)
	h.webAuditDecided(a, choice, by)
	where := "on the laptop"
	if by != "laptop" {
		where = "on " + by
	}
	if choice != webproto.ChoiceConfirm {
		m.webAuditLine(a.op, a.summary, a.device, "cancelled "+where)
		return nil
	}
	m.webAuditLine(a.op, a.summary, a.device, "confirmed "+where)
	return a.run(m)
}
