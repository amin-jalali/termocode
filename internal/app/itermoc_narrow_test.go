package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Itermoc at 80×24 (B6): the list and the page toggle. The list takes the
// whole width (rows gain a middle column and the inline fix pill); Enter
// opens the page full-width; Esc returns to the list.

// imNarrowModel is the overview test data in an 80×24 window, revealed.
func imNarrowModel(t *testing.T) *Model {
	t.Helper()
	m := imOverviewModel(t)
	m.w, m.h = 80, 24
	m.revealItermocView(true, "")
	return m
}

func imKey(m *Model, key string) (tea.Cmd, bool) {
	var k tea.KeyMsg
	switch key {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return m.itermocPageKey(k)
}

// Spec: a narrow window shows Itermoc as one full-width page that starts
// on the list: no sidebar, the keys in the page.
func TestItermocNarrowOpensTheList(t *testing.T) {
	m := imNarrowModel(t)
	if m.showExp {
		t.Error("the sidebar is still shown at 80 columns")
	}
	if p := m.itermocPage; p == nil || !p.list {
		t.Fatalf("page %+v, want the list", m.itermocPage)
	}
	if m.focus != FocusEditor {
		t.Errorf("focus %v, want the page", m.focus)
	}
	if got := m.itermocPageTitle(); got != aiSpark()+" Itermoc" {
		t.Errorf("tab title %q", got)
	}
	// A wide window keeps the sidebar list.
	w := imOverviewModel(t)
	w.revealItermocView(true, "")
	if !w.showExp || (w.itermocPage != nil && w.itermocPage.list) {
		t.Error("a wide window lost the sidebar list")
	}
}

// Spec: the full-width list has the middle column (the steps, the model,
// what a service waits for) and the inline fix pill; every row is the
// pane width.
func TestItermocNarrowListRows(t *testing.T) {
	m := imNarrowModel(t)
	f := imPageText(m, 74, 40)
	for _, want := range []string{"ITERMOC", "Overview", "3 !", "PROFILES", "review", "Reviewer ▶ Fixer", "active",
		"AI PROVIDERS", "Anthropic", "API key · API · pay per use", "2 models", "from config.json", "openai · gpt-5-mini",
		"Set key (k)", "SERVICES", "sign-in expired", "Sign in (i)",
		"Enter open"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	for _, sz := range [][2]int{{74, 21}, {58, 21}, {36, 21}} {
		for i, l := range m.itermocPageLayout(sz[0], sz[1]).rows {
			if w := ansi.StringWidth(l); w != sz[0] {
				t.Errorf("%dx%d: row %d is %d wide", sz[0], sz[1], i, w)
			}
		}
	}
}

// Spec: below 60 columns the right column drops first, then the middle
// one; the glyph and the name always stay.
func TestItermocNarrowColumnsDrop(t *testing.T) {
	m := imNarrowModel(t)
	f := imPageText(m, 58, 40)
	if strings.Contains(f, "Sign in (i)") || strings.Contains(f, " active") {
		t.Errorf("58 columns kept the right column:\n%s", f)
	}
	if !strings.Contains(f, "sign-in expired") {
		t.Errorf("58 columns lost the middle column:\n%s", f)
	}
	f = imPageText(m, 36, 40)
	if strings.Contains(f, "sign-in expired") {
		t.Errorf("36 columns kept the middle column:\n%s", f)
	}
	for _, want := range []string{"review", "linear", "gpt"} {
		if !strings.Contains(f, want) {
			t.Errorf("36 columns lost the name %q:\n%s", want, f)
		}
	}
}

// Spec: in the list j moves, Enter opens the row's page full-width and
// Esc returns to the list on the same row.
func TestItermocNarrowEnterAndEsc(t *testing.T) {
	m := imNarrowModel(t)
	if r, _ := m.itermoc.current(); r.kind != imRowOverview {
		t.Fatalf("cursor on %+v", r)
	}
	imKey(m, "j") // the PROFILES header
	imKey(m, "j") // the first profile
	r, _ := m.itermoc.current()
	if r.kind != imRowProfile {
		t.Fatalf("j j: cursor on %+v", r)
	}
	if _, ok := imKey(m, "enter"); !ok {
		t.Fatal("Enter not taken")
	}
	p := m.itermocPage
	if p.list || p.sec != imSecProfiles || p.name != r.name {
		t.Fatalf("Enter: page %+v, want %s", p, r.name)
	}
	f := imPageText(m, 74, 40)
	if !strings.Contains(f, "Esc back") {
		t.Errorf("the page does not say Esc goes back:\n%s", f)
	}
	imKey(m, "esc")
	if !m.itermocPage.list {
		t.Fatal("Esc did not return to the list")
	}
	if c, _ := m.itermoc.current(); imRowKey(c) != imRowKey(r) {
		t.Errorf("back on %+v, want %+v", c, r)
	}
	// The overview row opens the overview; Esc comes back.
	m.itermoc.cursorTo(imRow{kind: imRowOverview, sec: imSecOverview, name: imSecOverview})
	imKey(m, "enter")
	if p := m.itermocPage; p.list || p.sec != imSecOverview {
		t.Errorf("Enter on Overview: %+v", p)
	}
}

// Spec: a click on a list row opens its page; a click on the inline pill
// runs the fix.
func TestItermocNarrowClicks(t *testing.T) {
	m := imNarrowModel(t)
	ids := imHitIDs(m, 74, 40)
	for _, id := range []string{"im:pg:row:services/linear", "im:pg:rowfix:services/linear", "im:pg:row:overview/overview"} {
		if !imHasHit(ids, id) {
			t.Errorf("no hit %s in %v", id, ids)
		}
	}
	m.itermocListClick("row:services/linear")
	if p := m.itermocPage; p.list || p.sec != imSecServices || p.name != "linear" {
		t.Errorf("row click: page %+v", p)
	}
	m = imNarrowModel(t)
	m.itermocListClick("rowfix:profiles/house")
	if pt := m.aiS().profTrust; pt == nil || pt.name != "house" {
		t.Errorf("pill click: trust card %+v", pt)
	}
	if !m.itermocPage.list {
		t.Error("the fix left the list")
	}
}
