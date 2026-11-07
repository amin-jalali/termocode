package dbclient

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInlineArgsSkipsQuotesAndComments(t *testing.T) {
	got, err := inlineArgs(`SELECT '?', "a?" FROM t WHERE x = ? -- why?
AND y = ?`, []any{"it's", 7})
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT '?', "a?" FROM t WHERE x = 'it''s' -- why?
AND y = 7`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if _, err := inlineArgs("SELECT ?", nil); err != nil {
		t.Fatalf("no args must leave the query alone: %v", err)
	}
	if _, err := inlineArgs("SELECT ?, ?", []any{1}); err == nil {
		t.Fatal("too few args must fail")
	}
	if _, err := inlineArgs("SELECT ?", []any{1, 2}); err == nil {
		t.Fatal("too many args must fail")
	}
}

func TestSQLLiteral(t *testing.T) {
	for v, want := range map[any]string{nil: "NULL", true: "TRUE", 3: "3", int64(-4): "-4", 1.5: "1.5", "a'b": "'a''b'"} {
		if got := sqlLiteral(v); got != want {
			t.Errorf("sqlLiteral(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestReadDuckDBJSONKeepsColumnOrder(t *testing.T) {
	out := []byte(`[{"z":1,"a":"x","n":null,"l":[1,2]},
{"z":2,"a":"y","n":3,"l":[]}]
`)
	r := readDuckDBJSON(out, 0)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	var names []string
	for _, c := range r.Columns {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "z,a,n,l" {
		t.Fatalf("columns = %v", names)
	}
	if len(r.Rows) != 2 || r.Rows[0][1].Text != "x" || !r.Rows[0][2].Null || r.Rows[0][3].Kind != "json" || r.Rows[1][2].Text != "3" {
		t.Fatalf("rows = %+v", r.Rows)
	}
}

func TestReadDuckDBJSONLastResultAndTruncate(t *testing.T) {
	out := []byte(`[{"a":1}]
[{"b":1},{"b":2},{"b":3}]
`)
	r := readDuckDBJSON(out, 2)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if len(r.Columns) != 1 || r.Columns[0].Name != "b" || len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("result = %+v", r)
	}
	if r := readDuckDBJSON(nil, 0); r.Err != nil || len(r.Rows) != 0 {
		t.Fatalf("empty output = %+v", r)
	}
	if r := readDuckDBJSON([]byte("oops"), 0); r.Err == nil {
		t.Fatal("bad output must fail")
	}
}

func TestDuckDBArgsKeepSQLOffTheCommandLine(t *testing.T) {
	args := strings.Join(duckdbArgs("/data/x.duckdb", true), " ")
	if !strings.Contains(args, "-json") || !strings.Contains(args, "-readonly") || !strings.HasSuffix(args, "/data/x.duckdb") {
		t.Fatalf("args = %s", args)
	}
	if strings.Contains(strings.Join(duckdbArgs("x", false), " "), "-readonly") {
		t.Fatal("a writable session must not pass -readonly")
	}
}

// fakeDuckDB is a shell script standing in for the duckdb client: it
// records its arguments and stdin, then prints canned JSON.
func fakeDuckDB(t *testing.T, body string) (logDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "duckdb")
	script := "#!/bin/sh\necho \"$@\" > '" + dir + "/args'\ncat > '" + dir + "/stdin'\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := lookupDuckDB
	lookupDuckDB = func() (string, error) { return bin, nil }
	t.Cleanup(func() { lookupDuckDB = old })
	return dir
}

func TestDuckDBSessionRunsTheClient(t *testing.T) {
	dir := fakeDuckDB(t, `echo '[{"one":1}]'`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "/tmp/x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := s.Run(context.Background(), "SELECT ? AS one", RunOptions{Args: []any{1}})
	if r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "1" {
		t.Fatalf("result = %+v", r)
	}
	in, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	if string(in) != "SELECT 1 AS one" {
		t.Fatalf("stdin = %q", in)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if strings.Contains(string(args), "SELECT") || strings.Contains(string(args), "-readonly") {
		t.Fatalf("args = %q", args)
	}
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	if r := ro.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	args, _ = os.ReadFile(filepath.Join(dir, "args"))
	if !strings.Contains(string(args), "-readonly") {
		t.Fatalf("read-only args = %q", args)
	}
	if err := s.Begin(context.Background()); err == nil {
		t.Fatal("Begin must say there are no transactions")
	}
}

func TestDuckDBSessionErrorAndMissingClient(t *testing.T) {
	fakeDuckDB(t, `case "$(cat "$(dirname "$0")/stdin")" in "SELECT 1") echo '[{"1":1}]';; *) echo 'Parser Error: syntax error at "BAD"' >&2; exit 1;; esac`)
	s, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x.duckdb"})
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Run(context.Background(), "BAD", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "Parser Error") {
		t.Fatalf("err = %v", r.Err)
	}
	_ = s.Close()
	if r := s.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err == nil {
		t.Fatal("a closed session must refuse")
	}
	lookupDuckDB = func() (string, error) { return "", os.ErrNotExist }
	if _, err := NewDuckDBSession(context.Background(), Conn{Driver: "duckdb", Path: "x"}); err == nil || !strings.Contains(err.Error(), "duckdb is not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestDuckDBWiring(t *testing.T) {
	if _, ok := DialectFor("duckdb").(DuckDBDialect); !ok {
		t.Fatal("duckdb has no dialect")
	}
	if CLITool("duckdb") != "duckdb" {
		t.Fatal("no duckdb CLI tool")
	}
	cmd, err := CLICommand(Conn{Driver: "duckdb", Path: "db/x.duckdb", ReadOnly: true}, CLIOptions{Root: "/w"})
	if err != nil || strings.Join(cmd.Args, " ") != "-readonly "+filepath.Join("/w", "db/x.duckdb") {
		t.Fatalf("cmd = %+v, err = %v", cmd, err)
	}
	if err := Validate(Conn{ID: "d", Driver: "duckdb"}); err == nil {
		t.Fatal("a duckdb connection needs a path")
	}
	if err := Validate(Conn{ID: "d", Driver: "duckdb", Path: "x.duckdb"}); err != nil {
		t.Fatalf("a duckdb file needs no host: %v", err)
	}
	if got := (DuckDBDialect{}).QuoteIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("quote = %s", got)
	}
}
