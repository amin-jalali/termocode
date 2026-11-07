package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type dbSessCloser struct {
	dbclient.Session
	closed *int
}

func (s dbSessCloser) Close() error { *s.closed++; return nil }

// dbSessionsModel has local-pg with its main session and a session of
// database "audit"; the audit session has a transaction with 2 writes.
func dbSessionsModel(t *testing.T) (*Model, *int, *int) {
	t.Helper()
	m := &Model{w: 120, h: 40}
	m.databases = newDBState()
	m.databases.loaded = true
	m.databases.conns = []dbclient.Conn{{ID: "local-pg", Name: "local-pg", Driver: "postgres"}}
	mainClosed, auditClosed := 0, 0
	m.databases.sessions["local-pg"] = dbSessCloser{closed: &mainClosed}
	m.databases.sessions["local-pg/db/audit"] = dbSessCloser{closed: &auditClosed}
	tx := m.databases.tx("local-pg/db/audit")
	tx.open, tx.writes = true, 2
	return m, &mainClosed, &auditClosed
}

// Databases: Sessions… lists every open session with its transaction.
func TestDBSessionsPickerLists(t *testing.T) {
	m, _, _ := dbSessionsModel(t)
	cmd, ok := m.dispatchDatabasesPalette("db-sessions")
	if !ok {
		t.Fatal("db-sessions not dispatched")
	}
	_ = cmd
	if !m.pickerOpen || m.pickerKind != pickerKindDBSessions {
		t.Fatal("no sessions picker")
	}
	view := ansi.Strip(m.picker.View())
	for _, want := range []string{"local-pg", "audit", "2 not committed"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker misses %q:\n%s", want, view)
		}
	}
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-sessions"
	}
	if !found {
		t.Fatal("db-sessions not in the palette")
	}
}

// Enter on a session asks first (Cancel focused); Cancel keeps it; Close
// closes only that session and drops its transaction.
func TestDBSessionsCloseOne(t *testing.T) {
	m, mainClosed, auditClosed := dbSessionsModel(t)
	m.dispatchDatabasesPalette("db-sessions")
	if _, ok := m.handleHTTPPickerSelect("local-pg/db/audit"); !ok {
		t.Fatal("pick not routed")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindDBSessionClose {
		t.Fatal("no close card")
	}
	if !strings.Contains(ansi.Strip(m.confirm.View()), "2") {
		t.Fatalf("card must say the changes are rolled back:\n%s", ansi.Strip(m.confirm.View()))
	}
	m.confirmOpen = false
	m.databaseConsoleConfirm("cancel")
	if *auditClosed != 0 || m.databases.sessions["local-pg/db/audit"] == nil {
		t.Fatal("cancel closed the session")
	}
	m.dispatchDatabasesPalette("db-sessions")
	m.handleHTTPPickerSelect("local-pg/db/audit")
	m.confirmOpen = false
	m.databaseConsoleConfirm("close")
	if *auditClosed != 1 || m.databases.sessions["local-pg/db/audit"] != nil {
		t.Fatal("session not closed")
	}
	if m.databases.txOpen("local-pg/db/audit") {
		t.Fatal("transaction still marked open")
	}
	if *mainClosed != 0 || m.databases.sessions["local-pg"] == nil {
		t.Fatal("the main session must stay")
	}
}

// The first (focused) button of the card is Cancel.
func TestDBSessionsCardSafeFirst(t *testing.T) {
	m, _, _ := dbSessionsModel(t)
	m.dispatchDatabasesPalette("db-sessions")
	m.handleHTTPPickerSelect("local-pg/db/audit")
	if b := m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
		t.Fatalf("buttons = %+v, want Cancel first", b)
	}
}

// No sessions: a toast, no picker.
func TestDBSessionsNone(t *testing.T) {
	m := &Model{w: 120, h: 40}
	m.databases = newDBState()
	m.databases.loaded = true
	m.dispatchDatabasesPalette("db-sessions")
	if m.pickerOpen {
		t.Fatal("picker opened with no sessions")
	}
}
