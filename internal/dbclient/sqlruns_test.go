package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DBX-45: saved SQL runs (.termocode/sql-runs.json).

func TestParseSQLRuns(t *testing.T) {
	runs, err := ParseSQLRuns([]byte(`{
	  // a comment
	  "runs": [{"name": " nightly ", "script": "sql/r.sql", "connections": ["a", "b"], "before": ["make seed"], "log": "logs/r.log"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Name != "nightly" || len(runs[0].Connections) != 2 || runs[0].Before[0] != "make seed" || runs[0].Log != "logs/r.log" {
		t.Fatalf("runs: %+v", runs)
	}
}

func TestParseSQLRunsRefusals(t *testing.T) {
	for name, src := range map[string]string{
		"no name":       `{"runs":[{"script":"a.sql","connections":["a"]}]}`,
		"twice":         `{"runs":[{"name":"x","script":"a.sql","connections":["a"]},{"name":"x","script":"b.sql","connections":["a"]}]}`,
		"no script":     `{"runs":[{"name":"x","connections":["a"]}]}`,
		"no connection": `{"runs":[{"name":"x","script":"a.sql"}]}`,
		"empty step":    `{"runs":[{"name":"x","script":"a.sql","connections":["a"],"before":[" "]}]}`,
		"line break":    `{"runs":[{"name":"x","script":"a.sql","connections":["a"],"before":["ls\nrm -rf /"]}]}`,
		"bad json":      `{"runs":[`,
	} {
		if _, err := ParseSQLRuns([]byte(src)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLoadSQLRunsMissingFile(t *testing.T) {
	runs, err := LoadSQLRuns(t.TempDir())
	if err != nil || runs != nil {
		t.Fatalf("runs %v err %v", runs, err)
	}
	root := t.TempDir()
	p := SQLRunsPath(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"runs":[{"name":"x","script":"a.sql","connections":["a"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if runs, err := LoadSQLRuns(root); err != nil || len(runs) != 1 {
		t.Fatalf("runs %v err %v", runs, err)
	}
}

func TestSQLRunShellKeepsPasswordsOutOfTheLine(t *testing.T) {
	r := SQLRun{Name: "n", Script: "sql/r.sql", Connections: []string{"a", "b"}, Before: []string{"make seed"}, Log: "logs/r.log"}
	targets := []SQLRunTarget{
		{Name: "PG one", CLI: CLICmd{Tool: "psql", Args: []string{"-h", "db"}, Env: []string{"PGPASSWORD=s3cret'1"}}},
		{Name: "PG two", CLI: CLICmd{Tool: "psql", Args: []string{"-h", "db2"}, Env: []string{"PGPASSWORD=other"}}},
	}
	line, env, err := SQLRunShell(r, targets, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "s3cret") || strings.Contains(line, "other") {
		t.Errorf("a password is on the line: %s", line)
	}
	if env["TC_SQLRUN_1_PGPASSWORD"] != "s3cret'1" || env["TC_SQLRUN_2_PGPASSWORD"] != "other" {
		t.Errorf("env: %v", env)
	}
	for _, want := range []string{
		"set -e",
		"make seed",
		`PGPASSWORD="$TC_SQLRUN_1_PGPASSWORD" 'psql' '-h' 'db' < '/work/sql/r.sql'`,
		`PGPASSWORD="$TC_SQLRUN_2_PGPASSWORD" 'psql' '-h' 'db2' < '/work/sql/r.sql'`,
		"mkdir -p '/work/logs' && ",
		"| tee -a '/work/logs/r.log'",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q:\n%s", want, line)
		}
	}
	if strings.Index(line, "make seed") > strings.Index(line, "'db'") || strings.Index(line, "'db'") > strings.Index(line, "'db2'") {
		t.Errorf("order: before steps, then the connections in order:\n%s", line)
	}
}

func TestSQLRunShellWithoutLog(t *testing.T) {
	line, _, err := SQLRunShell(SQLRun{Name: "n", Script: "/abs/a.sql", Connections: []string{"f"}},
		[]SQLRunTarget{{Name: "file", CLI: CLICmd{Tool: "sqlite3", Args: []string{"/w/app.db"}}}}, "/w")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(line, "tee") || !strings.Contains(line, "'sqlite3' '/w/app.db' < '/abs/a.sql'") {
		t.Errorf("line: %s", line)
	}
	if _, _, err := SQLRunShell(SQLRun{Name: "n", Script: "a.sql"}, nil, "/w"); err == nil {
		t.Error("no targets should be refused")
	}
}

func TestSQLRunSummary(t *testing.T) {
	s := SQLRunSummary(SQLRun{Script: "a.sql", Before: []string{"make seed"}, Log: "r.log"}, []string{"A", "B"})
	for _, want := range []string{"a.sql", "2 connection(s)", "A, B", "$ make seed", "r.log", "first error stops"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
}
