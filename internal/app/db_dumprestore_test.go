package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-19: dump and restore from the palette with the server's own tools
// (found on PATH), behind a Cancel-first card; Excel in the export picker;
// JSON files in the import picker.

func dbDumpModel(t *testing.T, driver string) (*Model, *[]dbclient.CLICmd) {
	t.Helper()
	m := hoverModel(t)
	m.databases = newDBState()
	m.databases.conns = []dbclient.Conn{{ID: "c", Name: "c", Driver: driver, Host: "h", User: "u", Database: "shop"}}
	m.databases.passwords["c"] = "pw"
	m.dbConsole().shown = &dbRun{conn: "c"}
	var ran []dbclient.CLICmd
	oldLook, oldRun, oldConn := dbToolLookPath, dbToolRun, dbDumpConnected
	dbToolLookPath = func(tool string) (string, error) { return "/usr/bin/" + tool, nil }
	dbToolRun = func(_ context.Context, path string, c dbclient.CLICmd) (string, error) {
		ran = append(ran, c)
		return "", nil
	}
	dbDumpConnected = func(*Model, string) bool { return true }
	t.Cleanup(func() { dbToolLookPath, dbToolRun, dbDumpConnected = oldLook, oldRun, oldConn })
	return &m, &ran
}

func TestDBDumpPaletteEntries(t *testing.T) {
	titles := map[string]string{}
	for _, it := range databasesPaletteItems() {
		titles[it.ID] = it.Title
	}
	for _, id := range []string{"db-dump", "db-restore"} {
		if titles[id] == "" {
			t.Errorf("%s not in the palette", id)
		}
		if p, ok := aiActionPolicies[id]; !ok || p.Gate == aiGateAuto {
			t.Errorf("%s AI policy = %+v %v", id, p, ok)
		}
	}
}

func TestDBDumpAsksThenRunsTool(t *testing.T) {
	m, ran := dbDumpModel(t, "postgres")
	m.dispatchDatabasesPalette("db-dump")
	if !m.promptOpen || m.promptKind != promptKindDBDumpToolPath {
		t.Fatalf("prompt=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	out := filepath.Join(t.TempDir(), "shop.dump")
	m.handleHTTPPromptSubmit(out)
	if !m.confirmOpen || m.confirmKind != confirmKindDBDumpTool {
		t.Fatalf("card=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	if strings.Contains(m.confirm.View(), "PGPASSWORD") {
		t.Error("card shows the password")
	}
	// Cancel first: nothing runs.
	m.confirmOpen = false
	m.databaseConsoleConfirm("cancel")
	if len(*ran) != 0 {
		t.Fatalf("cancel ran %v", *ran)
	}

	m.dispatchDatabasesPalette("db-dump")
	m.promptOpen = false
	m.handleHTTPPromptSubmit(out)
	m.confirmOpen = false
	cmd, _ := m.databaseConsoleConfirm("dump")
	if cmd == nil {
		t.Fatal("dump gave no command")
	}
	msg := cmd()
	if done, ok := msg.(dbToolDoneMsg); !ok || done.err != nil {
		t.Fatalf("msg = %#v", msg)
	}
	if len(*ran) != 1 || (*ran)[0].Tool != "pg_dump" {
		t.Fatalf("ran = %+v", *ran)
	}
}

func TestDBDumpMissingTool(t *testing.T) {
	m, _ := dbDumpModel(t, "mysql")
	dbToolLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	m.dispatchDatabasesPalette("db-dump")
	if m.promptOpen {
		t.Fatal("prompt opened without mysqldump on PATH")
	}
}

func TestDBRestoreRefusesReadOnly(t *testing.T) {
	m, _ := dbDumpModel(t, "postgres")
	m.databases.conns[0].ReadOnly = true
	m.dispatchDatabasesPalette("db-restore")
	if m.promptOpen {
		t.Fatal("restore prompt opened on a read-only connection")
	}
}

func TestDBRestoreCardIsCancelFirst(t *testing.T) {
	m, ran := dbDumpModel(t, "postgres")
	in := filepath.Join(t.TempDir(), "shop.sql")
	_ = os.WriteFile(in, []byte("select 1;"), 0o600)
	m.dispatchDatabasesPalette("db-restore")
	m.promptOpen = false
	m.handleHTTPPromptSubmit(in)
	if !m.confirmOpen || m.confirmKind != confirmKindDBRestore {
		t.Fatalf("card=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	if !strings.Contains(m.confirm.View(), "Cancel") {
		t.Error("no Cancel button")
	}
	m.confirmOpen = false
	cmd, _ := m.databaseConsoleConfirm("restore")
	if cmd != nil {
		cmd()
	}
	if len(*ran) != 1 || (*ran)[0].Tool != "psql" {
		t.Fatalf("ran = %+v", *ran)
	}
}

func TestDBImportListsJSONFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.csv", "b.json", "c.jsonl", "d.txt"} {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600)
	}
	got := strings.Join(dbFindCSV(dir), ",")
	if got != "a.csv,b.json,c.jsonl" {
		t.Errorf("files = %s", got)
	}
}
