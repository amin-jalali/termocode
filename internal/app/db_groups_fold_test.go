package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/statusbar"
	"github.com/amin-jalali/termocode/internal/theme"
)

// DBX-21 (rest): a folder heading folds and unfolds, the fold survives a
// restart, and the connection colour tints the result page and the
// status-bar chip.

func dbFolderModel(t *testing.T) Model {
	t.Helper()
	m := hoverModel(t)
	s := newDBState()
	s.conns = []dbclient.Conn{
		{ID: "a", Name: "a"},
		{ID: "p1", Name: "p1", Folder: "prod", Color: "red"},
		{ID: "p2", Name: "p2", Folder: "prod"},
	}
	for _, c := range s.conns {
		s.nodes[c.ID] = dbNode{kind: dbNodeConnection, name: c.Name, path: c.ID, conn: c.ID}
	}
	s.loaded = true
	m.databases = s
	return m
}

func dbFoldRowNames(s *dbState) string {
	var got []string
	for _, r := range s.rows() {
		if r.kind == dbNodeFolderHeader {
			got = append(got, "#"+r.name)
		} else {
			got = append(got, r.name)
		}
	}
	return strings.Join(got, ",")
}

func TestDBFolderHeadingFolds(t *testing.T) {
	m := dbFolderModel(t)
	s := m.databases
	if got := dbFoldRowNames(s); got != "a,#prod,p1,p2" {
		t.Fatalf("rows = %s", got)
	}
	s.cursor = 1 // the "prod" heading
	m, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyLeft})
	if got := dbFoldRowNames(s); got != "a,#prod" {
		t.Fatalf("left on the heading: rows = %s", got)
	}
	m, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := dbFoldRowNames(s); got != "a,#prod,p1,p2" {
		t.Fatalf("enter on a folded heading: rows = %s", got)
	}
	m, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if got := dbFoldRowNames(s); got != "a,#prod" {
		t.Fatalf("space on an open heading: rows = %s", got)
	}
	m, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRight})
	if got := dbFoldRowNames(s); got != "a,#prod,p1,p2" {
		t.Fatalf("right on a folded heading: rows = %s", got)
	}

	// A key for a connection does nothing on a heading.
	m, cmd := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil || m.confirm != nil {
		t.Error("x on a heading asked something")
	}

	// A filter shows matches inside a folded folder.
	s.cursor = 1
	m, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyLeft})
	s.filter = "p2"
	if got := dbFoldRowNames(s); got != "#prod,p2" {
		t.Fatalf("filter in a folded folder: rows = %s", got)
	}
	s.filter = ""

	// The heading glyph follows the fold.
	view := m.renderDatabasesSidebar(40, 20)
	if !strings.Contains(view, theme.IconFolderClosed.String()+" prod") {
		t.Errorf("folded heading has no closed glyph:\n%s", view)
	}
}

func TestDBFolderFoldSurvivesRestart(t *testing.T) {
	withConfigDir(t)
	m := dbFolderModel(t)
	m.databases.folded[dbFolderPath("prod")] = true
	m.databasesSaveSession()
	if got := loadSession().DBFoldedFolders; strings.Join(got, ",") != "prod" {
		t.Fatalf("saved folded folders = %v", got)
	}
	if got := workspacePart(loadSession()).DBFoldedFolders; strings.Join(got, ",") != "prod" {
		t.Fatalf("workspace part lost the folded folders: %v", got)
	}
}

func TestDBFolderHeadingClickToggles(t *testing.T) {
	m := dbFolderModel(t)
	m.explorerWidth = 40
	// Row 0 of the tree is screen line 4; the heading is the second row.
	m, _ = m.handleDatabasesSidebarMouse(3, 5, tea.MouseLeft)
	if got := dbFoldRowNames(m.databases); got != "a,#prod" {
		t.Fatalf("click on the heading: rows = %s", got)
	}
	m, _ = m.handleDatabasesSidebarMouse(3, 5, tea.MouseLeft)
	if got := dbFoldRowNames(m.databases); got != "a,#prod,p1,p2" {
		t.Fatalf("second click: rows = %s", got)
	}
}

func TestDBResultPageConnTint(t *testing.T) {
	withConfigDir(t)
	m := dbGridModel(dbDisplayResult())
	m.databases = newDBState()
	m.databases.conns = []dbclient.Conn{{ID: "local", Name: "local", Color: "blue"}}
	p := m.databasePage()
	if !strings.HasPrefix(p.lines[0], dbConnBar()) {
		t.Fatalf("row 1 has no colour bar: %q", p.lines[0])
	}
	var groups []string
	for _, h := range p.hls {
		if h["l"] == 1 {
			groups = append(groups, h["g"].(string))
		}
	}
	if !strings.Contains(strings.Join(groups, ","), "TermocodeDbConnBlue") {
		t.Fatalf("row 1 groups = %v", groups)
	}

	// No colour: the page is as before.
	m.databases.conns[0].Color = ""
	if p := m.databasePage(); strings.HasPrefix(p.lines[0], dbConnBar()) {
		t.Fatalf("row 1 has a bar without a colour: %q", p.lines[0])
	}

	// Every colour has its highlight group in the theme.
	lua := themeLua("dark", false)
	for _, c := range dbclient.ConnColors() {
		if !strings.Contains(lua, "'"+dbConnTintGroup(c)+"'") {
			t.Errorf("theme has no group for %s", c)
		}
	}
}

func TestDBStatusChipTint(t *testing.T) {
	m := dbFolderModel(t)
	m.databases.activeID = "p1"
	st := m.statusState()
	if !st.ShowDatabase || !st.DatabaseTinted || st.DatabaseTint != theme.DiagError {
		t.Fatalf("status = %+v", statusbar.State{ShowDatabase: st.ShowDatabase, DatabaseTinted: st.DatabaseTinted, DatabaseTint: st.DatabaseTint})
	}
	m.databases.activeID = "p2"
	if st := m.statusState(); st.DatabaseTinted {
		t.Error("a connection without a colour tints the chip")
	}
}
