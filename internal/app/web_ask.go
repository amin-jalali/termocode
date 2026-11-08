package app

import (
	"fmt"

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
//
// The page approves (owner 2026-11-08): the same person who paired the
// device can do everything on the web that the laptop can. Unless the
// laptop's web_approve_on_laptop is on, a controller page's request is
// not a laptop card: the page that asked gets the card's exact text as
// an ask ev, Cancel first, and confirms or cancels it with cards.answer.
// Only that connection may confirm; it ends with it. The laptop gets a
// passive toast of what was done, never a dialog; the Web channel and
// the gateway's audit say it was approved in the page. Viewers never
// reach this: every LaptopCard op needs a controller.

// confirmKindWebAsk is the laptop's dialog of a laptop card.
const confirmKindWebAsk confirmKindEnum = 2610

// webMaxAsks caps the laptop cards waiting at once.
const webMaxAsks = 4

// webApproveOnLaptopSetting reads web_approve_on_laptop (default off).
func webApproveOnLaptopSetting() bool { return webSettings().ApproveOnLaptop }

// webApproveOnLaptop is what a new hub takes; the package's tests swap it
// (main_test.go) for the laptop-card tests written before the page
// approved.
var webApproveOnLaptop = webApproveOnLaptopSetting

func init() {
	// Registered before the cards publisher (file order), so a card that
	// opened, closed or moved in this Update is listed right.
	webAfterUpdates = append(webAfterUpdates, (*Model).webAskPump)
}

// webAskFunc checks a request whose op needs a laptop card and returns
// the card; nil means it answered the call itself (a refusal).
type webAskFunc func(m *Model, call *webCall) *webAsk

// webAskOps are the card builders of the ops whose policy row says
// LaptopCard. The hub runs such an op only through its card: a plain
// handler in webOps is never used for it (web_call.go).
var webAskOps = map[string]webAskFunc{}

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
	// asker is the page that confirms it (the page approves); nil: a
	// laptop card. id is then its id.
	asker *webConn
	id    string
}

// webAskState is the line of laptop cards (Update only).
type webAskState struct {
	line  []*webAsk          // oldest first
	shown *webAsk            // the one whose dialog is open on the laptop
	here  map[string]*webAsk // the pages' own questions by id (the page approves)
	n     int                // the last page question's number
}

// askHere: a request on c is confirmed by its own page, not the laptop.
func (h *webHub) askHere(c *webConn) bool {
	return !h.approveOnLaptop && c != nil && c.role == webproto.RoleController
}

// webAskOpen puts a laptop card in line for call and answers with its id.
func (m *Model) webAskOpen(call *webCall, a *webAsk) {
	if id, ok := m.webAskQueue(call, a); ok {
		call.reply(webproto.AskRef{Card: id})
	}
}

// webAskQueue puts a laptop card in line for call and returns its id; an
// op that answers with more than the id replies itself. ok=false: it
// answered the call (too many cards wait).
func (m *Model) webAskQueue(call *webCall, a *webAsk) (string, bool) {
	h := m.web
	if h.askHere(call.conn) {
		return m.webAskHereQueue(call, a)
	}
	if len(h.asks.line) >= webMaxAsks {
		call.fail(webproto.CodeBusy, "Too many questions wait for the laptop.")
		return "", false
	}
	a.op, a.conn, a.device = call.op, call.conn.id, webName(call.conn.device)
	if a.device == "" {
		a.device = "web"
	}
	a.body += "\n\nRequested from " + a.device + "."
	h.asks.line = append(h.asks.line, a)
	id, _ := h.cards.id(a)
	m.webAskShow()
	return id, true
}

// webAskHereQueue asks the page that sent call to confirm a itself: the
// ask ev with the card's exact text goes to that page only.
func (m *Model) webAskHereQueue(call *webCall, a *webAsk) (string, bool) {
	s := &m.web.asks
	if call.ctx.Err() != nil { // the page left while the card was built
		call.fail(webproto.CodeGone, "The connection ended.")
		return "", false
	}
	if len(s.here) >= webMaxAsks {
		call.fail(webproto.CodeBusy, "Too many questions wait. Answer one first.")
		return "", false
	}
	s.n++
	id := fmt.Sprintf("ask-%d", s.n)
	data, err := webData(webproto.AskCard{ID: id, Title: a.title, Body: a.body, Action: a.action})
	if err != nil {
		recordError("[web] " + call.op + ": ask: " + err.Error())
		call.fail(webproto.CodeInternal, "Something went wrong.")
		return "", false
	}
	a.op, a.conn, a.device, a.asker, a.id = call.op, call.conn.id, webName(call.conn.device), call.conn, id
	if a.device == "" {
		a.device = "web"
	}
	if s.here == nil {
		s.here = map[string]*webAsk{}
	}
	s.here[a.id] = a
	call.conn.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAsk, Data: data})
	return a.id, true
}

// webAskHereAnswer is cards.answer on a page question: only the page
// that asked may answer it, with confirm or cancel.
func (m *Model) webAskHereAnswer(call *webCall, a *webAsk, choice string) tea.Cmd {
	if call.conn != a.asker {
		call.fail(webproto.CodeForbidden, "Only the page that asked can answer this.")
		return nil
	}
	if choice != webproto.ChoiceConfirm && choice != webproto.ChoiceCancel {
		call.fail(webproto.CodeBadRequest, "That is not an answer to this question.")
		return nil
	}
	cmd := m.webAskDone(a, choice, a.device)
	call.reply(nil)
	return cmd
}

// webAskHereGone cancels the questions of a page whose connection ended.
func (m *Model) webAskHereGone(h *webHub, c *webConn) {
	if m.web != h {
		return
	}
	for _, a := range h.asks.here {
		if a.asker == c {
			m.webAskDone(a, webproto.ChoiceCancel, a.device)
		}
	}
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
	if a.asker != nil {
		return m.webAskHereDone(a, choice)
	}
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

// webAskHereDone ends a page question: the page hears answered, the
// audit says it was decided in the page, and confirm runs the action
// with a passive toast on the laptop.
func (m *Model) webAskHereDone(a *webAsk, choice string) tea.Cmd {
	h := m.web
	if h.asks.here[a.id] != a {
		return nil // answered already
	}
	delete(h.asks.here, a.id)
	if data, err := webData(webproto.Answered{ID: a.id, By: a.device, Choice: choice}); err == nil {
		a.asker.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: data})
	}
	h.webAuditDecided(a, choice, a.device)
	where := "on " + a.device + " (page)"
	if choice != webproto.ChoiceConfirm {
		m.webAuditLine(a.op, a.summary, a.device, "cancelled "+where)
		return nil
	}
	m.webAuditLine(a.op, a.summary, a.device, "confirmed "+where)
	var t tea.Cmd
	m.toast, t = m.toast.PushDetail(toast.Info, "Approved on "+a.device, a.title)
	return tea.Batch(a.run(m), t)
}
