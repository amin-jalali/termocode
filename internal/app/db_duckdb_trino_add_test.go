package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBTypePickerListsDuckDBTrinoPresto(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range dbTypePickerItems(false) {
		ids[it.ID] = true
	}
	for _, want := range []string{"duckdb", "trino", "presto"} {
		if !ids[want] {
			t.Errorf("the add picker has no %s row", want)
		}
	}
	remote := map[string]bool{}
	for _, it := range dbTypePickerItems(true) {
		remote[it.ID] = true
	}
	if !remote["trino"] || !remote["presto"] || remote["duckdb"] || remote["sqlite"] {
		t.Errorf("remote picker = %v (servers only)", remote)
	}
}

func TestDBAddDuckDBAsksForAFile(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("duckdb")
	if m.promptKind != promptKindDBSQLitePath || !strings.HasSuffix(m.prompt.Value(), ".duckdb") {
		t.Fatalf("no DuckDB file prompt: kind=%v value=%q", m.promptKind, m.prompt.Value())
	}
	if s.pendingConn.Driver != "duckdb" {
		t.Fatalf("pending = %+v", s.pendingConn)
	}
	m.onDBPrompt(promptKindDBSQLitePath, "")
	if !strings.Contains(covSvcToast(m), "DuckDB file") {
		t.Fatalf("toast = %q", covSvcToast(m))
	}
	m.onDBPrompt(promptKindDBSQLitePath, "data/sales.duckdb")
	if c := s.pendingConn; c.ID != "sales" || !strings.HasSuffix(c.Path, "sales.duckdb") {
		t.Fatalf("pending = %+v", c)
	}
}

func TestDBAddTrinoAsksForTheCatalog(t *testing.T) {
	for _, kind := range []string{"trino", "presto"} {
		m, _ := covSvcDB(t, "")
		s := m.databases
		m.databaseStartAdd(kind)
		if m.promptKind != promptKindDBHost {
			t.Fatalf("%s: no host prompt", kind)
		}
		if c := s.pendingConn; c.Driver != kind || c.Port != 8080 || c.User == "" {
			t.Fatalf("%s: pending = %+v", kind, c)
		}
		m.onDBPrompt(promptKindDBHost, "coord")
		m.onDBPrompt(promptKindDBPort, "8080")
		m.onDBPrompt(promptKindDBUser, "ana")
		if m.promptKind != promptKindDBName || !strings.Contains(m.prompt.View(), "Catalog") {
			t.Fatalf("%s: no catalog prompt: %v", kind, m.promptKind)
		}
	}
}

func TestDBTestAllowsDuckDBAndTrino(t *testing.T) {
	for _, c := range []dbclient.Conn{
		{ID: "d", Driver: "duckdb", Path: "missing.duckdb", ReadOnly: true},
		{ID: "t", Driver: "trino", Host: "127.0.0.1", Port: 1, User: "u", Database: "hive"},
		{ID: "p", Driver: "presto", Host: "127.0.0.1", Port: 1, User: "u", Database: "hive"},
	} {
		msg, _ := dbTestCommandSSH(c, "", t.TempDir(), 0, dbSSHSecrets{}, false)().(dbTestMsg)
		if msg.err == nil || strings.Contains(msg.err.Error(), "not available yet") {
			t.Fatalf("%s: err = %v", c.Driver, msg.err)
		}
	}
}

func TestDBDriverTitlesAndFileNames(t *testing.T) {
	for d, want := range map[string]string{"duckdb": "DuckDB", "trino": "Trino", "presto": "Presto"} {
		if got := dbDriverTitle(d); got != want {
			t.Errorf("dbDriverTitle(%s) = %q", d, got)
		}
	}
	if dbFileEngineName("duckdb") != "DuckDB" || dbFileEngineName("sqlite") != "SQLite" {
		t.Fatal("file engine names")
	}
	if dbConnDetail(dbclient.Conn{Driver: "duckdb", Path: "x.duckdb"}) != "duckdb" {
		t.Fatal("duckdb detail")
	}
}
