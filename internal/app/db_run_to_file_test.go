package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Run to File takes only one plain read: a write or two statements would
// run again (or run at all) without the user seeing the rows.
func TestDBRunToFileSQLOnlyOnePlainRead(t *testing.T) {
	if sql, why := dbRunToFileSQL("postgres", "-- @conn pg\nSELECT * FROM users;\n"); why != "" || sql != "SELECT * FROM users;" {
		t.Fatalf("read: %q %q", sql, why)
	}
	for _, bad := range []string{"DELETE FROM users", "SELECT 1; SELECT 2", "  ", "UPDATE users SET a = 1"} {
		if _, why := dbRunToFileSQL("postgres", bad); why == "" {
			t.Errorf("%q must be refused", bad)
		}
	}
	if _, why := dbRunToFileSQL("redis", "GET a"); why == "" {
		t.Error("a Redis command must be refused")
	}
}

// The picker has every format but SQL UPDATEs: a query has no table key.
func TestDBRunToFileFormatsHaveNoUpdate(t *testing.T) {
	var csv bool
	for _, f := range dbRunToFileFormats() {
		if f.Format == dbclient.FormatSQLUpdate {
			t.Fatal("SQL UPDATEs offered for a query")
		}
		csv = csv || f.Format == dbclient.FormatCSV
	}
	if !csv {
		t.Fatal("no CSV")
	}
}

// runCmds runs a command and every command a batch holds, feeding the
// messages back to the model.
func runCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmds(m, c)
		}
	case dbExportDoneMsg:
		m.onDBExportDone(msg)
	}
}

// The whole flow: format → file → a card with Cancel first → the rows of
// the query streamed into the file, the grid left alone.
func TestDBRunToFileWritesTheQueryRows(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}}
	s.sessions["pg"] = &searchSession{}
	m := dbTestModel(s)
	if cmd := m.databaseRunToFileBegin("pg", `SELECT * FROM "users"`); cmd != nil {
		t.Fatal("a toast instead of the picker")
	}
	e := m.dbConsole().exp
	if !m.pickerOpen || m.pickerKind != pickerKindDBExportFormat || e == nil || !e.toFile || !e.all {
		t.Fatalf("no format picker: open=%v kind=%v exp=%+v", m.pickerOpen, m.pickerKind, e)
	}
	m.databaseExportFormatPicked(string(dbclient.FormatCSV))
	if !m.promptOpen || m.promptKind != promptKindDBExportPath {
		t.Fatal("no file prompt")
	}
	path := filepath.Join(t.TempDir(), "users.csv")
	m.databaseExportPathTyped(path)
	if !m.confirmOpen || m.confirmKind != confirmKindDBRunToFile {
		t.Fatalf("no run card: open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	if b := m.confirm.Buttons(); len(b) == 0 || b[0].ID != "cancel" {
		t.Fatalf("Cancel must be first: %+v", b)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("written before the card was answered")
	}
	cmd, ok := m.databaseTransferConfirm("run")
	if !ok {
		t.Fatal("the card is not routed")
	}
	runCmds(m, cmd)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "id") || !strings.Contains(string(b), "2") {
		t.Fatalf("file: %q", b)
	}
	if m.dbConsole().shown != nil {
		t.Fatal("the grid changed")
	}
}

// Cancel on the card writes nothing and forgets the export.
func TestDBRunToFileCancel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}}
	s.sessions["pg"] = &searchSession{}
	m := dbTestModel(s)
	m.databaseRunToFileBegin("pg", `SELECT * FROM "users"`)
	m.databaseExportFormatPicked(string(dbclient.FormatJSON))
	path := filepath.Join(t.TempDir(), "users.json")
	m.databaseExportPathTyped(path)
	m.databaseTransferConfirm("cancel")
	if m.dbConsole().exp != nil {
		t.Fatal("the export is kept")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a file was written")
	}
}

// Not connected: a toast, no picker.
func TestDBRunToFileNeedsASession(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "pg", Driver: "postgres"}}
	m := dbTestModel(s)
	m.databaseRunToFileBegin("pg", "SELECT 1")
	if m.pickerOpen || m.dbConsole().exp != nil {
		t.Fatal("a picker for a closed connection")
	}
}
