//go:build unix

package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
)

// Spec (AH-1, old 5.2): the start card has Not now first and focused;
// Enter on a fresh card and Esc are Not now; nothing starts.
func TestWebStartCardSafeFirst(t *testing.T) {
	r := newWebPanelRig(t)
	m := r.m
	m.openWebStartCard(nil)
	if !m.confirmOpen || m.confirmKind != confirmKindWebStart || m.confirm.SafeID() != "not-now" {
		t.Fatalf("card open %v kind %v safe %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	var ids []string
	for _, b := range m.confirm.Buttons() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, ",") != "not-now,open,copy" {
		t.Fatalf("buttons %v", ids)
	}
	_, cmd := m.confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sel, _ := cmd().(confirm.SelectMsg)
	if sel.ID != "not-now" {
		t.Fatalf("Enter picks %q", sel.ID)
	}
	m.answerWebStart(sel.ID)
	m.answerWebStart("") // Esc
	if m.webPanel != nil {
		t.Fatal("the panel started on Enter or Esc")
	}
	body := webStartBody(webSettings(), false)
	for _, want := range []string{"run any command as you", "type into them", "127.0.0.1", "not masked", "15 minutes"} {
		if !strings.Contains(body, want) {
			t.Errorf("card text has no %q", want)
		}
	}
}

// Spec (T52): `termocode web` without --yes shows the start card on the
// laptop; Copy link there starts the panel and the CLI gets the port; Not
// now answers forbidden.
func TestCLIOpenShowsCard(t *testing.T) {
	r := newWebPanelRig(t)
	ctl := r.dial(`{"t":"ctl"}`)
	ctl.send(`{"t":"req","id":1,"op":"web.open"}`)
	r.pumpUntil("the start card", func() bool { return r.m.confirmOpen })
	if r.m.confirmKind != confirmKindWebStart || !strings.Contains(r.m.confirm.View(), "termocode web") {
		t.Fatalf("kind %v", r.m.confirmKind)
	}
	r.m.confirmOpen = false
	r.m.answerWebStart("not-now")
	if m := r.nextMsg(ctl); m["t"] != "err" || m["code"] != "forbidden" {
		t.Fatalf("got %v", m)
	}

	ctl.send(`{"t":"req","id":2,"op":"web.open"}`)
	r.pumpUntil("the start card", func() bool { return r.m.confirmOpen })
	r.m.confirmOpen = false
	r.m.answerWebStart("copy")
	for {
		m := r.nextMsg(ctl)
		if m["t"] == "res" && m["id"] == float64(2) {
			break
		}
	}
	select {
	case l := <-r.links:
		if !strings.Contains(l, "#s=") {
			t.Fatalf("link %q", l)
		}
	default:
		t.Fatal("Copy link copied nothing")
	}
}

// Spec (T42, WEB-10): the AI can never reach a web palette command.
func TestAIActionsNeverWeb(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range webPaletteItems() {
		ids[it.ID] = true
		p, ok := aiActionPolicies[it.ID]
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s: the AI may run it", it.ID)
		}
	}
	for _, id := range []string{"web-start", "web-stop", "web-copy-link"} {
		if !ids[id] {
			t.Errorf("no palette entry %s", id)
		}
	}
}
