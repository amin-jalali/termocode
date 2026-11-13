package dbclient

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func fakeSession(t *testing.T, f *fakeSQL, d Dialect, name string, readonly bool) *SQLSession {
	t.Helper()
	return newSQLSession(f.open(t), d, name, Conn{}, readonly)
}

func oneRow() fakeSet {
	return fakeSet{cols: []string{"n"}, rows: [][]driver.Value{{int64(1)}}}
}

// A read-only session opens the engine's own read-only transaction with
// its own timeout setting, runs the read and rolls back.
func TestReadOnlyBeginIsEngineSpecific(t *testing.T) {
	for _, c := range []struct {
		name    string
		d       Dialect
		mariadb bool
		want    string
	}{
		{"mysql", MySQLDialect{}, false, "max_execution_time = 30000"},
		{"mysql", MySQLDialect{}, true, "max_statement_time = 30"},
		{"sqlserver", SQLServerDialect{}, false, "SET LOCK_TIMEOUT 30000"},
		{"postgres", PostgresDialect{}, false, "statement_timeout = '30s'"},
		{"firebird", FirebirdDialect{}, false, ""},
		{"exasol", ExasolDialect{}, false, ""},
	} {
		f := (&fakeSQL{}).on("select 1", oneRow())
		s := fakeSession(t, f, c.d, c.name, true)
		s.mariadb = c.mariadb
		res := s.Run(context.Background(), "SELECT 1", RunOptions{})
		if res.Err != nil || len(res.Rows) != 1 {
			t.Errorf("%s: %+v", c.name, res)
			continue
		}
		ran := strings.Join(f.ran(), " | ")
		if c.want != "" && !strings.Contains(ran, c.want) {
			t.Errorf("%s ran %q, want %q", c.name, ran, c.want)
		}
		if !strings.Contains(ran, "ROLLBACK") {
			t.Errorf("%s: a read-only run ends with a rollback: %s", c.name, ran)
		}
	}
}

func TestReadOnlyBeginFailureIsReportedAndRolledBack(t *testing.T) {
	f := (&fakeSQL{}).fail("set transaction", errFake).fail("begin", errFake).fail("start transaction", errFake).fail("set lock_timeout", errFake)
	for name, d := range map[string]Dialect{"postgres": PostgresDialect{}, "sqlserver": SQLServerDialect{}} {
		s := fakeSession(t, f, d, name, true)
		if res := s.Run(context.Background(), "SELECT 1", RunOptions{}); res.Err == nil {
			t.Errorf("%s: a failing begin must be an error", name)
		}
	}
	if !strings.Contains(strings.Join(f.ran(), "|"), "ROLLBACK") {
		t.Error("a failed begin is rolled back")
	}
}

func TestSQLSessionRefusalsAndSettings(t *testing.T) {
	f := (&fakeSQL{}).on("select", oneRow())
	s := fakeSession(t, f, PostgresDialect{}, "postgres", true)
	if res := s.Run(context.Background(), "DELETE FROM t", RunOptions{}); res.Err == nil || !strings.Contains(res.Err.Error(), "read-only") {
		t.Errorf("a write in a read-only session = %v", res.Err)
	}
	s.KeepIdle(2, 0)
	rw := fakeSession(t, f, PostgresDialect{}, "postgres", false)
	if err := rw.AsReadOnly(); err != nil {
		t.Fatal(err)
	}
	if res := rw.Run(context.Background(), "INSERT INTO t VALUES (1)", RunOptions{}); res.Err == nil {
		t.Error("AsReadOnly makes the session refuse writes")
	}
	busy := fakeSession(t, f, PostgresDialect{}, "postgres", false)
	if err := busy.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := busy.AsReadOnly(); err == nil || !strings.Contains(err.Error(), "transaction") {
		t.Errorf("AsReadOnly in a transaction = %v", err)
	}
	_ = busy.Rollback(context.Background())
	closed := fakeSession(t, f, PostgresDialect{}, "postgres", false)
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closed.AsReadOnly(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("AsReadOnly after Close = %v", err)
	}
}

func TestSQLServerCellsAndBinaryNotice(t *testing.T) {
	guid := []byte{0x78, 0x56, 0x34, 0x12, 0xab, 0x90, 0xef, 0xcd, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0}
	f := (&fakeSQL{}).on("select", fakeSet{
		cols: []string{"id", "price", "doc", "raw"},
		rows: [][]driver.Value{{guid, []byte("9.99"), []byte("<a/>"), []byte{0, 1, 2}}},
	})
	s := fakeSession(t, f, SQLServerDialect{}, "sqlserver", false)
	res := s.Run(context.Background(), "SELECT 1", RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 {
		t.Fatalf("%+v", res)
	}
	if len(res.Columns) != 4 {
		t.Fatalf("columns = %+v", res.Columns)
	}
	if c, ok := sqlServerCell(guid, "uniqueidentifier"); !ok || len(c.Text) != 36 {
		t.Errorf("guid cell = %+v %v", c, ok)
	}
	if c, ok := sqlServerCell([]byte("1.5"), "MONEY"); !ok || c.Kind != "number" {
		t.Errorf("money cell = %+v %v", c, ok)
	}
	if c, ok := sqlServerCell([]byte("<x/>"), "xml"); !ok || c.Text != "<x/>" {
		t.Errorf("xml cell = %+v %v", c, ok)
	}
	if _, ok := sqlServerCell([]byte("short"), "uniqueidentifier"); ok {
		t.Error("a GUID of the wrong size stays generic")
	}
	if _, ok := sqlServerCell("text", "money"); ok {
		t.Error("only bytes are re-read")
	}
	if _, ok := sqlServerCell([]byte("x"), "int"); ok {
		t.Error("other types stay generic")
	}
}

func TestSessionExplainRollsBackWrites(t *testing.T) {
	f := (&fakeSQL{}).on("explain", oneRow()).on("begin", fakeSet{}).on("rollback", fakeSet{})
	s := fakeSession(t, f, PostgresDialect{}, "postgres", false)
	if res := s.Explain(context.Background(), "SELECT 1", false); res.Err != nil {
		t.Errorf("explain = %v", res.Err)
	}
	// ANALYZE of a write runs inside a transaction that is rolled back.
	if res := s.Explain(context.Background(), "DELETE FROM t", true); res.Err != nil {
		t.Errorf("analyze of a write = %v", res.Err)
	}
	ran := strings.Join(f.ran(), " | ")
	if !strings.Contains(ran, "EXPLAIN (ANALYZE") {
		t.Errorf("the analyzed form must run: %s", ran)
	}
	if s.tx != nil {
		t.Error("the transaction of an analyzed write must be rolled back, not left open")
	}
	s.mariadb = true
	if res := s.Explain(context.Background(), "SELECT 1", true); res.Err != nil {
		t.Errorf("mariadb analyze = %v", res.Err)
	}
	if !strings.Contains(strings.Join(f.ran(), "|"), "ANALYZE SELECT 1") {
		t.Error("MariaDB uses ANALYZE <statement>")
	}
}
