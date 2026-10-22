package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
	tea "github.com/charmbracelet/bubbletea"
)

func dbNoSQLiteModel(w, h int) (Model, *dbState) {
	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "app-db", Name: "app.db", Driver: "sqlite", Path: "app.db"}}
	note, code := "app-db/sqlite-note", "app-db/sqlite-install"
	s.nodes["app-db"] = dbNode{kind: dbNodeConnection, name: "app.db", path: "app-db", conn: "app-db", loaded: true, children: []string{note, code}}
	s.nodes[note] = dbNode{kind: dbNodeNote, name: "sqlite3 is not installed or is too old", path: note, conn: "app-db"}
	s.nodes[code] = dbNode{kind: dbNodeCode, name: dbclient.SQLiteInstallHintWithRelease("linux", "amd64", "ID=debian"), path: code, conn: "app-db"}
	return Model{databases: s, w: w, h: h, showExp: true, activity: activity.New(), explorerWidth: 32}, s
}

// DB-1: the install note and command wrap in the tree, and y copies the command.
func TestDatabaseSQLiteInstallHintWrapsAndCopies(t *testing.T) {
	m, s := dbNoSQLiteModel(80, 24)
	got := stripSGRForTest(m.renderDatabasesSidebarRaw(31, 20, true))
	flat := strings.Join(strings.Fields(strings.NewReplacer("\n", " ").Replace(got)), " ")
	for _, want := range []string{"sqlite3 is not installed or is too old", "sudo apt install sqlite3"} {
		if !strings.Contains(flat, want) {
			t.Fatalf("sidebar lost %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "…") {
		t.Fatalf("a row is cut:\n%s", got)
	}
	for _, cursor := range []int{0, 2} {
		s.cursor = cursor
		got = stripSGRForTest(m.renderDatabasesSidebarRaw(31, 20, true))
		if !strings.Contains(got, "y copy command") {
			t.Fatalf("cursor %d: footer has no copy key:\n%s", cursor, got)
		}
	}
	s.cursor = 0
	var copied []string
	defer SetClipboardSink(func(v string) { copied = append(copied, v) })()
	_, _ = m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if len(copied) != 1 || copied[0] != "sudo apt install sqlite3" {
		t.Fatalf("copied %q, want only the command", copied)
	}
}

// A wrapped row takes several lines; a click on its second line still picks it.
func TestDatabaseWrappedRowMouseHit(t *testing.T) {
	m, s := dbNoSQLiteModel(80, 24)
	m.renderDatabasesSidebarRaw(31, 20, true)
	// lines 0-3 header; row 0 = line 4; note wraps over 2 lines (5,6); code starts at 7.
	_, _ = m.handleDatabasesSidebarMouse(3, 6, tea.MouseLeft)
	if s.cursor != 1 {
		t.Fatalf("click on the wrapped note's second line picked row %d, want 1", s.cursor)
	}
	_, _ = m.handleDatabasesSidebarMouse(3, 7, tea.MouseLeft)
	if s.cursor != 2 {
		t.Fatalf("click on the command picked row %d, want 2", s.cursor)
	}
}
