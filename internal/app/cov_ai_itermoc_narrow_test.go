package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The full-width Itermoc list of a narrow window (itermoc_narrow.go):
// Enter on each kind of row, the keys it lets through, the wheel and
// clicks on rows and section headers.

func covCursorKind(t *testing.T, m *Model, kind imRowKind) imRow {
	t.Helper()
	for _, r := range m.itermoc.rows() {
		if r.kind == kind && r.selectable() {
			m.itermoc.cursorTo(r)
			return r
		}
	}
	t.Fatalf("no row of kind %v", kind)
	return imRow{}
}

func TestCovAIItermocNarrowEnterOnEveryRow(t *testing.T) {
	m := imNarrowModel(t)
	sec := covCursorKind(t, m, imRowSection)
	if _, ok := imKey(m, "enter"); !ok || !m.itermoc.collapse[sec.sec] {
		t.Errorf("Enter on %s did not fold it", sec.sec)
	}
	imKey(m, "enter")
	if m.itermoc.collapse[sec.sec] {
		t.Error("the second Enter did not unfold it")
	}
	covCursorKind(t, m, imRowAdd)
	if _, ok := imKey(m, "o"); !ok {
		t.Error("o on an add row was not taken")
	}
	m.pickerOpen, m.promptOpen, m.connectOpen, m.profileFormOpen = false, false, false, false
	m.itermocPage.list = true
	covCursorKind(t, m, imRowProvider)
	if _, ok := imKey(m, "enter"); !ok {
		t.Error("Enter on a provider was not taken")
	}
	m.itermocPage.list = true
	if _, ok := imKey(m, "esc"); !ok {
		t.Error("Esc on the list was not taken")
	}
	if _, ok := m.itermocPageKey(tea.KeyMsg{Type: tea.KeyDown}); !ok {
		t.Error("down on the list was not taken")
	}
	if _, ok := m.itermocPageKey(tea.KeyMsg{Type: tea.KeyF5}); ok {
		t.Error("F5 was taken by the list")
	}
}

// A filter that matches nothing says so instead of an empty list.
func TestCovAIItermocNarrowEmptyFilter(t *testing.T) {
	m := imNarrowModel(t)
	m.itermoc.filter = "zzz-nothing-matches"
	if f := imPageText(m, 74, 30); !strings.Contains(f, `No rows match "zzz-nothing-matches" · Esc clears`) {
		t.Errorf("empty filter page:\n%s", f)
	}
}

// The wheel moves the cursor; a click on a row opens it; a click on a
// section header folds it; a click on nothing is still the list's.
func TestCovAIItermocNarrowMouse(t *testing.T) {
	m := imNarrowModel(t)
	m.applyLayout()
	pane := m.editorPaneLayout()
	before := m.itermoc.cursor
	press := func(x, y int, b tea.MouseButton) bool {
		nm, _, ok := m.routeToItermocPage(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
		*m = nm
		return ok
	}
	if !press(pane.X+1, pane.Top+1, tea.MouseButtonWheelDown) || m.itermoc.cursor == before {
		t.Errorf("wheel down: cursor %d → %d", before, m.itermoc.cursor)
	}
	if !press(pane.X+1, pane.Top+1, tea.MouseButtonWheelUp) || m.itermoc.cursor != before {
		t.Errorf("wheel up: cursor %d", m.itermoc.cursor)
	}
	if _, _, ok := m.routeToItermocPage(tea.MouseMsg{X: pane.X + 1, Y: pane.Top + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease}); ok {
		t.Error("a release was taken")
	}
	var row, secHit *hitTarget
	for _, h := range m.itermocPageTargets(pane) {
		h := h
		if row == nil && strings.HasPrefix(h.ID, "im:pg:row:profiles/") && !strings.Contains(h.ID, "/+") {
			row = &h
		}
		if secHit == nil && strings.HasPrefix(h.ID, "im:pg:sec:") {
			secHit = &h
		}
	}
	if row == nil {
		t.Fatal("no profile row target")
	}
	var probe string
	m.mouseProbe = &probe
	press(row.X, row.Y, tea.MouseButtonLeft)
	if probe != row.ID {
		t.Errorf("probe = %q, want %q", probe, row.ID)
	}
	m.mouseProbe = nil
	press(row.X, row.Y, tea.MouseButtonLeft)
	if m.itermocPage.list || m.itermocPage.sec != imSecProfiles {
		t.Errorf("row click: page %+v", m.itermocPage)
	}
	m.itermocPage.list = true
	if secHit != nil {
		key := strings.TrimPrefix(secHit.ID, "im:pg:sec:")
		m.itermocListClick("sec:" + key)
		if !m.itermoc.collapse[key] {
			t.Errorf("section click did not fold %s", key)
		}
	}
	if !press(pane.X+pane.W-1, pane.Top+pane.H-1, tea.MouseButtonLeft) {
		t.Error("a click on empty space of the list was not taken")
	}
	if m.itermocListClick("row:nope/nothing") != nil {
		t.Error("a click on a row that is gone did something")
	}
	add := covCursorKind(t, m, imRowAdd)
	m.itermocListClick("row:" + add.sec + "/" + add.name)
}
