package dbclient

import (
	"context"
	"strings"
	"testing"
	"time"
)

const duckIntrospectScript = `q="$(cat "$(dirname "$0")/stdin")"
case "$q" in
  *information_schema.schemata*) echo '[{"schema_name":"main"},{"schema_name":null},{"schema_name":"extra"}]';;
  *information_schema.tables*) echo '[{"table_name":"t","kind":"BASE TABLE"},{"table_name":"v","kind":"VIEW"}]';;
  *information_schema.columns*) echo '[{"column_name":"id","data_type":"INTEGER"},{"column_name":"name","data_type":"VARCHAR"}]';;
  *duckdb_tables*) echo '[{"sql":"CREATE TABLE t(id INTEGER, name VARCHAR);"}]';;
  *) echo '[]';;
esac`

func TestDuckDBIntrospectLevels(t *testing.T) {
	fakeDuckDB(t, duckIntrospectScript)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(top.Databases, ",") != "main,extra" {
		t.Errorf("schemas = %+v, %v", top, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Database: "main"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[1].Kind != "VIEW" {
		t.Errorf("objects = %+v, %v", objs, err)
	}
	// An object with no schema reads main.
	cols, err := s.Introspect(ctx, IntrospectionRequest{Object: "t"})
	if err != nil || len(cols.Columns) != 2 || cols.Columns[1].Type != "VARCHAR" || !strings.HasPrefix(cols.DDL, "CREATE TABLE t") {
		t.Errorf("columns = %+v, %v", cols, err)
	}
}

func TestDuckDBIntrospectFailures(t *testing.T) {
	fakeDuckDB(t, `q="$(cat "$(dirname "$0")/stdin")"
case "$q" in "SELECT 1") echo '[{"1":1}]';; *) echo 'Catalog Error' >&2; exit 1;; esac`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, req := range map[string]IntrospectionRequest{
		"schemas": {}, "objects": {Schema: "main"}, "columns": {Schema: "main", Object: "t"},
	} {
		if _, err := s.Introspect(ctx, req); err == nil || !strings.Contains(err.Error(), "Catalog Error") {
			t.Errorf("%s = %v", name, err)
		}
	}
}

func TestDuckDBRunErrorsAndOddOutput(t *testing.T) {
	fakeDuckDB(t, `q="$(cat "$(dirname "$0")/stdin")"
case "$q" in
  "SELECT 1") echo '[{"1":1}]';;
  "silent") exit 3;;
  "notarray") echo '{"a":1}';;
  "cut") printf '[{"a":1},';;
  "cutrow") printf '[{"a":';;
  "cutend") printf '[{"a":1}';;
  "rowstring") echo '["x"]';;
  *) echo '[]';;
esac`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if r := s.Run(ctx, "silent", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "exit status 3") {
		t.Errorf("a client that fails silently = %v", r.Err)
	}
	for _, q := range []string{"notarray", "cut", "cutrow", "cutend", "rowstring"} {
		if r := s.Run(ctx, q, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "could not read the DuckDB result") {
			t.Errorf("%s = %v", q, r.Err)
		}
	}
	if r := s.Run(ctx, "other ?", RunOptions{Args: []any{1, 2}}); r.Err == nil {
		t.Error("too many arguments: want error")
	}
	if _, err := NewDuckDBSession(ctx, Conn{Driver: "duckdb"}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Errorf("no path = %v", err)
	}
}

func TestDuckDBCancelAndPingFailure(t *testing.T) {
	fakeDuckDB(t, `q="$(cat "$(dirname "$0")/stdin")"
case "$q" in "SELECT 1") echo '[{"1":1}]';; "slow") sleep 1;; *) echo '[]';; esac`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if r := s.Run(ctx, "slow", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "cancelled") {
		t.Errorf("a cancelled run = %v", r.Err)
	}
	s.Cancel() // nothing running: no effect
	fakeDuckDB(t, `echo 'cannot open file' >&2; exit 1`)
	if _, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"}); err == nil || !strings.Contains(err.Error(), "cannot open file") {
		t.Errorf("a file that cannot be opened = %v", err)
	}
}

func TestInlineArgsOddQuotes(t *testing.T) {
	for q, want := range map[string]string{
		`SELECT 'unterminated ?`: `SELECT 'unterminated ?`,
		"SELECT ? -- last":       "SELECT 7 -- last",
		"SELECT 'a''b?', ?":      "SELECT 'a''b?', 7",
	} {
		got, err := inlineArgs(q, []any{7})
		if err != nil && !strings.Contains(err.Error(), "arguments") {
			t.Errorf("%q: %v", q, err)
			continue
		}
		if err == nil && got != want {
			t.Errorf("inlineArgs(%q) = %q, want %q", q, got, want)
		}
	}
}
