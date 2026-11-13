package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DBX-71: Firebird, Vertica, Exasol and SAP HANA in the add picker, the
// add flow and the AI tools.

func TestDBTypePickerListsExtraEngines(t *testing.T) {
	for _, remote := range []bool{false, true} {
		var ids []string
		for _, it := range dbTypePickerItems(remote) {
			ids = append(ids, it.ID)
		}
		got := strings.Join(ids, ",")
		for _, want := range []string{"engine:firebird", "engine:vertica", "engine:exasol", "engine:hana"} {
			if !strings.Contains(got, want) {
				t.Errorf("remote=%v picker lacks %s: %s", remote, want, got)
			}
		}
		if !strings.Contains(got, "engine:db2") {
			t.Errorf("Db2 not listed: %s", got)
		}
	}
}

func TestDBAddExtraEngineFlow(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("engine:firebird")
	if m.promptKind != promptKindDBHost {
		t.Fatal("no host prompt")
	}
	if c := s.pendingConn; c.Driver != "firebird" || c.Port != 3050 || c.User != "SYSDBA" {
		t.Fatalf("pending = %+v", c)
	}
	if !strings.Contains(m.prompt.Note(), "path or alias") {
		t.Fatalf("note = %q", m.prompt.Note())
	}
	m.onDBPrompt(promptKindDBHost, "fb.example")
	m.onDBPrompt(promptKindDBPort, "3050")
	m.onDBPrompt(promptKindDBUser, "SYSDBA")
	if m.promptKind != promptKindDBName {
		t.Fatalf("prompt = %v", m.promptKind)
	}
	m.onDBPrompt(promptKindDBName, "/data/app.fdb")
	if c := s.pendingConn; c.Database != "/data/app.fdb" || c.ID == "" || m.promptKind != promptKindDBPassword {
		t.Fatalf("after database: %+v prompt=%v", c, m.promptKind)
	}

	// Db2 asks for the database name.
	m.databaseStartAdd("engine:db2")
	if c := s.pendingConn; c.Driver != "db2" || c.Port != 50000 || c.User == "" || !strings.Contains(m.prompt.Note(), "-tags db2") {
		t.Fatalf("db2 pending = %+v note=%q", c, m.prompt.Note())
	}

	// Exasol and SAP HANA take an empty schema.
	m.databaseStartAdd("engine:hana")
	m.onDBPrompt(promptKindDBHost, "hana.example")
	m.onDBPrompt(promptKindDBPort, "39017")
	m.onDBPrompt(promptKindDBUser, "SYSTEM")
	m.onDBPrompt(promptKindDBName, "")
	if c := s.pendingConn; c.Driver != "hana" || c.Database != "" || c.ID == "" || m.promptKind != promptKindDBPassword {
		t.Fatalf("hana empty schema: %+v prompt=%v", c, m.promptKind)
	}
	if m.databaseStartAdd("engine:informix") != nil || s.pendingConn.Driver == "informix" {
		t.Fatal("an unknown engine started an add")
	}
}

func TestDBAIToolsTakeExtraEnginesButNotExasol(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".termocode"), 0o755)
	conns := `{"connections":[` +
		`{"id":"fb","driver":"firebird","host":"db","database":"employee"},` +
		`{"id":"vt","driver":"vertica","host":"db","database":"vmart"},` +
		`{"id":"hn","driver":"hana","host":"db"},` +
		`{"id":"d2","driver":"db2","host":"db","database":"SAMPLE"},` +
		`{"id":"ex","driver":"exasol","host":"db"}]}`
	if err := os.WriteFile(filepath.Join(root, ".termocode", "databases.json"), []byte(conns), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"fb", "vt", "hn"} {
		if _, err := dbAIConn(root, id); err != nil {
			t.Errorf("%s refused: %v", id, err)
		}
	}
	if _, err := dbAIConn(root, "ex"); err == nil || !strings.Contains(err.Error(), "read-only transaction") {
		t.Fatalf("exasol: %v", err)
	}
	if _, err := dbAIConn(root, "d2"); err == nil || !strings.Contains(err.Error(), "read-only transaction") {
		t.Fatalf("db2: %v", err)
	}
	if got := dbAIDialect("vertica").Explain("SELECT 1", false); got != "EXPLAIN SELECT 1" {
		t.Errorf("vertica AI explain = %q", got)
	}
	if got := dbAIDialect("firebird").Explain("SELECT 1", false); got != "" {
		t.Errorf("firebird AI explain = %q", got)
	}
	if dbDriverTitle("db2") != "IBM Db2" || dbDriverTitle("hana") != "SAP HANA" {
		t.Errorf("title = %q", dbDriverTitle("hana"))
	}
}
