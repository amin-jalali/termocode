package app

import (
	"strings"
	"testing"
)

func TestDBConsoleFileForCassandraIsCQL(t *testing.T) {
	ext, header, ft := dbConsoleFile("cassandra", "ring")
	if ext != ".cql" || !strings.HasPrefix(header, "-- @conn ring\n") || ft == "" {
		t.Fatalf("cassandra console = %q %q %q", ext, header, ft)
	}
	if ext, _, ft := dbConsoleFile("redis", "c"); ext != ".redis" || ft != "redis" {
		t.Fatalf("redis console = %q %q", ext, ft)
	}
	if ext, header, ft := dbConsoleFile("postgres", "pg"); ext != ".sql" || header != "-- @conn pg\n\n" || ft != "sql" {
		t.Fatalf("sql console = %q %q %q", ext, header, ft)
	}
}

func TestDBConsoleExtAcceptsCQL(t *testing.T) {
	for p, want := range map[string]bool{"a.cql": true, "b.CQL": true, "c.sql": true, "d.redis": true, "e.txt": false, "": false} {
		if got := isDBConsoleExt(p); got != want {
			t.Errorf("isDBConsoleExt(%q) = %v", p, got)
		}
	}
	if _, ok := supportedExtensions[".cql"]; !ok {
		t.Error(".cql is not a supported extension")
	}
}

func TestDBTypePickerListsCassandra(t *testing.T) {
	for _, it := range dbTypePickerItems(false) {
		if it.ID == "cassandra" && strings.Contains(it.Title, "Cassandra") {
			return
		}
	}
	t.Fatal("the add picker has no Cassandra row")
}

func TestDBAddCassandraAsksKeyspace(t *testing.T) {
	m, _ := covSvcDB(t, "")
	s := m.databases
	m.databaseStartAdd("cassandra")
	if c := s.pendingConn; c.Driver != "cassandra" || c.Port != 9042 {
		t.Fatalf("pending = %+v", c)
	}
	m.onDBPrompt(promptKindDBHost, "ring.local")
	m.onDBPrompt(promptKindDBPort, "9042")
	m.onDBPrompt(promptKindDBUser, "")
	if m.promptKind != promptKindDBName || !strings.Contains(m.prompt.View(), "Keyspace") {
		t.Fatalf("no keyspace prompt: %q", m.prompt.View())
	}
	m.onDBPrompt(promptKindDBName, "")
	if m.promptKind != promptKindDBPassword || s.pendingConn.ID == "" {
		t.Fatalf("an empty keyspace was refused: kind=%v id=%q toast=%q", m.promptKind, s.pendingConn.ID, covSvcToast(m))
	}
}

func TestDBLensCoversCQLFiles(t *testing.T) {
	for p, want := range map[string]bool{"q.sql": true, "ring.cql": true, "R.CQL": true, "c.redis": false, "a.txt": false} {
		if got := isDBLensExt(p); got != want {
			t.Errorf("isDBLensExt(%q) = %v", p, got)
		}
	}
	if !strings.Contains(dbConsoleLua, "cql='sql'") {
		t.Error("an opened .cql file does not get the sql filetype, so it has no statement lens")
	}
}

func TestDBInspectionsSkipCQL(t *testing.T) {
	if dbInspectsFile("ring.cql") || !dbInspectsFile("q.sql") {
		t.Fatal("SQL inspections must run on .sql files only (CQL is a different language)")
	}
}

func TestWebDBAddDefaultsCassandraPort(t *testing.T) {
	if p := webDBDefaultPort("cassandra"); p != 9042 {
		t.Fatalf("cassandra default port = %d", p)
	}
}
