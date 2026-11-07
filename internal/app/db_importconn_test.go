package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-22: Databases: Import Connections… picks ~/.pgpass, ~/.my.cnf or a
// typed file, shows what it found on a card (Cancel first, never a
// password), then saves them to the user's connections file.

func dbImportConnModel(t *testing.T) (*Model, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	m := hoverModel(t)
	m.databases = newDBState()
	m.databases.loaded = true
	m.databases.conns = []dbclient.Conn{{ID: "local", Name: "local", Driver: "postgres"}}
	return &m, home
}

func TestDBImportConnectionsPaletteEntry(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-import-connections"
	}
	if !found {
		t.Fatal("db-import-connections not in the palette")
	}
	if p, ok := aiActionPolicies["db-import-connections"]; !ok || p.Gate == aiGateAuto {
		t.Errorf("AI policy = %+v %v", p, ok)
	}
}

func TestDBImportConnectionsPickerListsKnownFiles(t *testing.T) {
	m, home := dbImportConnModel(t)
	_ = os.WriteFile(filepath.Join(home, ".pgpass"), []byte("db.example:5432:shop:app:s3cret\n"), 0o600)
	m.dispatchDatabasesPalette("db-import-connections")
	if !m.pickerOpen || m.pickerKind != pickerKindDBImportConn {
		t.Fatalf("picker=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	view := m.picker.View()
	if !strings.Contains(view, ".pgpass") || strings.Contains(view, ".my.cnf") {
		t.Errorf("picker = %s", view)
	}
	m.pickerOpen = false
	m.databaseConsolePicker(filepath.Join(home, ".pgpass"))
	if !m.confirmOpen || m.confirmKind != confirmKindDBImportConn {
		t.Fatalf("card=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	card := m.confirm.View()
	if strings.Contains(card, "s3cret") {
		t.Error("the card shows the password")
	}
	if !strings.Contains(card, "db.example") || !strings.Contains(card, "password") {
		t.Errorf("card = %s", card)
	}
}

func TestDBImportConnectionsSavesUserFile(t *testing.T) {
	m, home := dbImportConnModel(t)
	cnf := filepath.Join(home, "team.cnf")
	_ = os.WriteFile(cnf, []byte("[client]\nhost=mysql.internal\nuser=app\ndatabase=shop\n"), 0o600)
	m.dispatchDatabasesPalette("db-import-connections")
	m.pickerOpen = false
	m.databaseConsolePicker(dbImportConnTypePath)
	if !m.promptOpen || m.promptKind != promptKindDBImportConnPath {
		t.Fatalf("prompt=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	m.handleHTTPPromptSubmit(cnf)
	if !m.confirmOpen || m.confirmKind != confirmKindDBImportConn {
		t.Fatalf("card=%v kind=%v", m.confirmOpen, m.confirmKind)
	}

	// Cancel first: nothing is written.
	m.confirmOpen = false
	m.databaseConsoleConfirm("cancel")
	file := filepath.Join(home, ".config", "termocode", "db", "connections.json")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("cancel wrote the connections file")
	}

	m.dispatchDatabasesPalette("db-import-connections")
	m.pickerOpen = false
	m.databaseConsolePicker(dbImportConnTypePath)
	m.promptOpen = false
	m.handleHTTPPromptSubmit(cnf)
	m.confirmOpen = false
	m.databaseConsoleConfirm("import")
	b, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(b), "mysql.internal") {
		t.Fatalf("file = %s %v", b, err)
	}
	if len(m.databases.conns) != 2 {
		t.Errorf("conns = %d, want 2", len(m.databases.conns))
	}
}

func TestDBImportConnectionsNothingFound(t *testing.T) {
	m, home := dbImportConnModel(t)
	empty := filepath.Join(home, "empty.txt")
	_ = os.WriteFile(empty, []byte("nothing here\n"), 0o600)
	m.dispatchDatabasesPalette("db-import-connections")
	m.pickerOpen = false
	m.databaseConsolePicker(dbImportConnTypePath)
	m.promptOpen = false
	m.handleHTTPPromptSubmit(empty)
	if m.confirmOpen {
		t.Fatal("a card opened for a file with no connections")
	}
}
