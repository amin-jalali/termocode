package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// filterTreeModel is a Databases view with three connections.
func filterTreeModel(t *testing.T) Model {
	t.Helper()
	withConfigDir(t)
	s := newDBState()
	s.loaded = true
	for _, n := range []string{"analytics", "app", "billing"} {
		s.conns = append(s.conns, dbclient.Conn{ID: n, Name: n, Driver: "postgres"})
		s.nodes[n] = dbNode{kind: dbNodeConnection, name: n, path: n, conn: n, loaded: true}
	}
	m := *dbTestModel(s)
	m.w, m.h, m.focus = 100, 30, FocusExplorer
	return m
}

func dbKey(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = m.handleDatabasesSidebarKey(msg)
	}
	return m
}

func dbRowTexts(s *dbState) string {
	var names []string
	for _, r := range s.rows() {
		names = append(names, r.name)
	}
	return strings.Join(names, ",")
}

// X-2: "/" in Databases filters as you type, like Containers: no prompt.
func TestDatabasesSlashFiltersInline(t *testing.T) {
	m := filterTreeModel(t)
	m = dbKey(m, "/")
	if m.promptOpen || !m.databases.filterTyping {
		t.Fatalf("/ must start the inline filter (prompt open: %v, typing: %v)", m.promptOpen, m.databases.filterTyping)
	}
	m = dbKey(m, "a", "n")
	if got := dbRowTexts(m.databases); got != "analytics" {
		t.Fatalf("rows after typing \"an\": %q", got)
	}
	frame := stripSGRForTest(m.renderDatabasesSidebarRaw(40, 12, true))
	if !strings.Contains(strings.Split(frame, "\n")[0], "DATABASES · /an") {
		t.Errorf("the title does not show the filter:\n%s", frame)
	}
	// Enter keeps the filter and gives the keys back.
	m = dbKey(m, "enter")
	if m.databases.filterTyping || m.databases.filter != "an" {
		t.Fatalf("Enter: typing=%v filter=%q", m.databases.filterTyping, m.databases.filter)
	}
	m = dbKey(m, "j")
	if m.databases.filter != "an" {
		t.Errorf("j after Enter changed the filter: %q", m.databases.filter)
	}
	// Esc clears the filter; the next Esc leaves the view.
	m = dbKey(m, "esc")
	if m.databases.filter != "" || m.focus != FocusExplorer || dbRowTexts(m.databases) != "analytics,app,billing" {
		t.Fatalf("Esc: filter=%q focus=%v rows=%q", m.databases.filter, m.focus, dbRowTexts(m.databases))
	}
	m = dbKey(m, "esc")
	if m.focus != FocusEditor {
		t.Errorf("second Esc: focus=%v, want the editor", m.focus)
	}
}

// X-2: Esc while typing clears the text and stops typing; Backspace and
// Ctrl+U edit it; letters that are keys elsewhere (a, R) are text here.
func TestDatabasesFilterEditing(t *testing.T) {
	m := filterTreeModel(t)
	m = dbKey(m, "/", "a", "p", "p")
	if m.pickerOpen || m.databases.filter != "app" {
		t.Fatalf("typing app: filter=%q picker=%v (a must not open Add)", m.databases.filter, m.pickerOpen)
	}
	m = dbKey(m, "backspace")
	if m.databases.filter != "ap" {
		t.Errorf("Backspace: %q", m.databases.filter)
	}
	m = dbKey(m, "R")
	if m.databases.filter != "apR" {
		t.Errorf("R is text while typing: %q", m.databases.filter)
	}
	m = dbKey(m, "ctrl+u")
	if m.databases.filter != "" || !m.databases.filterTyping {
		t.Errorf("Ctrl+U: %q typing=%v", m.databases.filter, m.databases.filterTyping)
	}
	m = dbKey(m, "b", "esc")
	if m.databases.filter != "" || m.databases.filterTyping || m.focus != FocusExplorer {
		t.Errorf("Esc while typing: filter=%q typing=%v focus=%v", m.databases.filter, m.databases.filterTyping, m.focus)
	}
}

// X-2: nothing matches: one line says so.
func TestDatabasesFilterNoMatchSaysSo(t *testing.T) {
	m := filterTreeModel(t)
	m = dbKey(m, "/", "z", "z")
	frame := stripSGRForTest(m.renderDatabasesSidebarRaw(48, 14, true))
	if !strings.Contains(frame, "No names match \"zz\" · Esc clears") {
		t.Errorf("no sentence for an empty filter:\n%s", frame)
	}
}

// X-2: the filter is kept for the next start when it is final (Enter), not
// on every key.
func TestDatabasesFilterIsSavedWhenFinal(t *testing.T) {
	m := filterTreeModel(t)
	m = dbKey(m, "/", "b", "i")
	if got := loadSession().DBFilter; got != "" {
		t.Errorf("saved while typing: %q", got)
	}
	m = dbKey(m, "enter")
	if got := loadSession().DBFilter; got != "bi" {
		t.Errorf("not saved after Enter: %q", got)
	}
	dbKey(m, "esc")
	if got := loadSession().DBFilter; got != "" {
		t.Errorf("not cleared after Esc: %q", got)
	}
}

// X-2: "Databases: Filter…" starts the inline filter and gives the view the
// keys; the header's "/ filter" does the same.
func TestDatabasesPaletteFilterStartsInlineFilter(t *testing.T) {
	m := filterTreeModel(t)
	m.focus = FocusEditor
	m.databases.visible = true
	cmd, ok := m.dispatchDatabasesPalette("db-filter")
	_ = cmd
	if !ok || m.promptOpen || !m.databases.filterTyping || m.focus != FocusExplorer {
		t.Fatalf("palette filter: ok=%v prompt=%v typing=%v focus=%v", ok, m.promptOpen, m.databases.filterTyping, m.focus)
	}
}

// X-2: Containers and Databases share the key handling.
func TestInlineFilterStepIsShared(t *testing.T) {
	run := func(text string, typing bool, keys ...string) (string, bool) {
		for _, k := range keys {
			var msg tea.KeyMsg
			switch k {
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "down":
				msg = tea.KeyMsg{Type: tea.KeyDown}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			}
			text, typing, _ = inlineFilterStep(text, typing, msg)
		}
		return text, typing
	}
	if text, typing := run("", false, "/ab"); text != "ab" || !typing {
		t.Errorf("a paste of /ab: %q typing=%v", text, typing)
	}
	if _, _, handled := inlineFilterStep("x", true, tea.KeyMsg{Type: tea.KeyDown}); handled {
		t.Error("Down must move the cursor while typing")
	}
	if _, _, handled := inlineFilterStep("", false, tea.KeyMsg{Type: tea.KeyEsc}); handled {
		t.Error("Esc without a filter is not the filter's")
	}
	if text, typing := run("x", true, "esc"); text != "" || typing {
		t.Errorf("Esc: %q %v", text, typing)
	}
	if got := inlineFilterTitle("ab", true, true); got != "/ab_" {
		t.Errorf("ASCII title = %q", got)
	}
}
