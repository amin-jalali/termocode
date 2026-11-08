package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's next question comes to the page (owner 2026-11-08): a
// laptop dialog of a kind in webMirrorKinds that opens within
// webMirrorWindow of a page's action asks in that page too (the ask ev),
// as the laptop shows it, through the one path of web_ask.go. The
// page's yes presses the dialog's main button (never trust, never
// Always), its Cancel the dialog's safe answer. The laptop can still
// answer; the first answer wins and the page hears it. A dialog with no
// page action before it stays on the laptop. Ops whose run asks with
// such a dialog (webTUIDialogOps) get no page card first: the TUI's own
// question is the one the page answers.

// webTUIDialogOps are the laptop-card ops whose run asks with the TUI's
// own dialog (mirrored to the page).
var webTUIDialogOps = map[string]bool{
	webproto.OpWorktreesArchive: true,
	webproto.OpWorktreesDelete:  true,
}

// webMirrorKinds are the laptop dialogs a page may answer when its own
// action opened them.
var webMirrorKinds = map[confirmKindEnum]bool{
	confirmKindWTSleep:            true, // Sleep: agent tabs would end (Keep running first)
	confirmKindWTArchive:          true,
	confirmKindWTDelete:           true,
	confirmKindWTDeleteDirty:      true,
	confirmKindAIWorktreeConflict: true, // a merge of the agent's work hit your changes
	confirmKindServicesLocal:      true, // run this project's service command (the exact command)
}

// webMirrorWindow is how long after a page's action a laptop dialog
// counts as its follow-up.
const webMirrorWindow = 2 * time.Minute

// webAskOrigin is the last page that acted, and when.
type webAskOrigin struct {
	conn *webConn
	at   time.Time
}

// webMirrorButton is the button the page's yes presses: the dialog's
// primary or destructive one, else its first other one; never the safe
// answer, a trust button or Always ("" when there is none).
func webMirrorButton(d confirm.Model) (id, label string) {
	var first confirm.Button
	for _, b := range d.Buttons() {
		if b.ID == d.SafeID() || b.ID == "trust" || b.ID == "always" {
			continue
		}
		if b.Style == confirm.StylePrimary || b.Style == confirm.StyleDestructive {
			return b.ID, b.Title
		}
		if first.ID == "" {
			first = b
		}
	}
	return first.ID, first.Title
}

// webMirrorSame: the laptop still shows the dialog a mirrors.
func (m *Model) webMirrorSame(a *webAsk, kind confirmKindEnum) bool {
	return m.confirmOpen && m.confirmKind == kind && m.confirm.Title() == a.title && m.confirm.Message() == a.body
}

// webMirrorPump runs after every Update: a mirrored dialog the laptop
// answered (or that changed) ends in the page; a new follow-up dialog
// asks in the page that acted.
func (m *Model) webMirrorPump() {
	h := m.web
	s := &h.asks
	if a := s.mirror; a != nil && !m.webMirrorSame(a, a.kind) {
		s.mirror = nil
		if s.here[a.id] == a {
			delete(s.here, a.id)
			if data, err := webData(webproto.Answered{ID: a.id, By: "laptop"}); err == nil {
				a.asker.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: data})
			}
			m.webAuditLine(a.op, a.summary, a.device, "answered on the laptop")
		}
	}
	o := s.origin
	if s.mirror != nil || !m.confirmOpen || !webMirrorKinds[m.confirmKind] || o == nil ||
		webNow().Sub(o.at) > webMirrorWindow || !h.live[o.conn] || o.conn.ctx.Err() != nil {
		return
	}
	btn, label := webMirrorButton(m.confirm)
	if btn == "" {
		return
	}
	kind, safe := m.confirmKind, m.confirm.SafeID()
	a := &webAsk{op: "laptop.dialog", summary: m.confirm.Title(), title: m.confirm.Title(), body: m.confirm.Message(), action: label, kind: kind}
	press := func(id string) func(m *Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			if !m.webMirrorSame(a, kind) {
				return nil // the laptop answered first
			}
			m.web.asks.mirror = nil
			m.confirmOpen = false
			return m.handleConfirmSelect(id)
		}
	}
	a.run, a.cancel = press(btn), press(safe)
	if _, code, _ := m.webAskHereAdd(o.conn, a, true); code == "" {
		s.mirror = a
	}
}
