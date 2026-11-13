package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// A folder dialect is saved in the project and found for files below the
// folder; the connection and a @dialect line still win over it, and it
// wins over the hints in the text.
func TestDBSQLFolderDialect(t *testing.T) {
	root := t.TempDir()
	if err := dbSaveSQLFolderDialect(root, filepath.Join(root, "mig"), "mysql"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "mig", "v1", "a.sql")
	if got := dbSQLFolderDialect(root, file); got != "mysql" {
		t.Fatalf("folder dialect = %q", got)
	}
	if got := dbSQLFolderDialect(root, filepath.Join(root, "other", "b.sql")); got != "" {
		t.Fatalf("other folder = %q", got)
	}
	hints := "SELECT 1::int RETURNING id;"
	if d, how := dbSQLDialectFor(hints, "", "mysql"); d != "mysql" || how != "folder" {
		t.Fatalf("folder vs hints = %q %q", d, how)
	}
	if d, how := dbSQLDialectFor("-- @dialect sqlite\n"+hints, "", "mysql"); d != "sqlite" || how != "detected" {
		t.Fatalf("@dialect vs folder = %q %q", d, how)
	}
	if d, how := dbSQLDialectFor(hints, "postgres", "mysql"); d != "postgres" || how != "connection" {
		t.Fatalf("connection vs folder = %q %q", d, how)
	}
	if err := dbSaveSQLFolderDialect(root, filepath.Join(root, "mig"), ""); err != nil {
		t.Fatal(err)
	}
	if got := dbSQLFolderDialect(root, file); got != "" {
		t.Fatalf("cleared folder dialect = %q", got)
	}
	if err := dbSaveSQLFolderDialect(root, filepath.Dir(root), "mysql"); err == nil {
		t.Fatal("a folder outside the workspace was saved")
	}
}

// Highlight usages: an alias resolves in its own statement only; a table
// name in the whole file.
func TestDBSQLUsageScope(t *testing.T) {
	text := "SELECT o.id FROM orders o;\nSELECT o FROM t;\nSELECT * FROM orders;"
	spans, note := dbSQLUsageSpans(text, "postgres", "o", 3)
	if len(spans) != 2 || !strings.Contains(note, "alias of orders") {
		t.Fatalf("alias spans = %+v %q", spans, note)
	}
	spans, note = dbSQLUsageSpans(text, "postgres", "orders", 3)
	if len(spans) != 2 || strings.Contains(note, "alias") {
		t.Fatalf("table spans = %+v %q", spans, note)
	}
	cte := "WITH recent AS (SELECT 1) SELECT * FROM recent;\nSELECT recent FROM t;"
	if spans, note := dbSQLUsageSpans(cte, "postgres", "recent", 30); len(spans) != 2 || !strings.Contains(note, "CTE") {
		t.Fatalf("cte spans = %+v %q", spans, note)
	}
}

// Syntax errors are dropped for the dialects listed in db_sql_syntax_off;
// the other inspections stay.
func TestDBInspectSyntaxOff(t *testing.T) {
	s := dbConsoleSettings{SyntaxOff: []string{"MySQL"}}
	if !s.syntaxOff("mysql") || s.syntaxOff("postgres") {
		t.Fatal("syntaxOff by dialect")
	}
	diags := []map[string]any{{"severity": dbDiagError, "message": "syntax"}, {"severity": dbDiagWarn, "message": "unknown table"}}
	got := dbDropSyntaxDiags(diags)
	if len(got) != 1 || got[0]["message"] != "unknown table" {
		t.Fatalf("kept = %v", got)
	}
}

// SQL in strings: the picker rows carry the line; the new palette ids
// have an AI policy.
func TestDBEmbeddedSQLItems(t *testing.T) {
	src := "package x\nvar q = \"SELECT id FROM users\"\n"
	items := dbEmbeddedSQLItems(dbclient.FindEmbeddedSQL(src, "go"))
	if len(items) != 1 || items[0].ID != "2" || !strings.Contains(items[0].Title, "SELECT id") {
		t.Fatalf("items = %+v", items)
	}
	for _, id := range []string{"db-sql-folder-dialect", "db-highlight-usages", "db-embedded-sql"} {
		found := false
		for _, it := range databasesPaletteItems() {
			found = found || it.ID == id
		}
		if !found {
			t.Fatalf("%s not in the palette", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("%s has no AI policy", id)
		}
	}
	if _, ok := aiSettingPolicies["db_sql_syntax_off"]; !ok {
		t.Fatal("db_sql_syntax_off has no AI policy")
	}
}

// Check SQL in Strings: the issues become diagnostics at their line and
// column in the code file; db_sql_syntax_off drops the syntax ones.
func TestDBEmbeddedDiagnostics(t *testing.T) {
	sc := dbclient.InspectSchema{Tables: map[string]map[string]bool{"": {"users": true}}, Complete: true, Columns: map[string][]string{"users": {"id", "name"}}}
	src := "package x\n\nvar u = \"SELECT u.nme FROM users u WHERE (1\"\n"
	got := dbEmbeddedDiagnostics(src, "go", sc, false)
	if len(got) != 2 {
		t.Fatalf("diags = %v", got)
	}
	if got[0]["lnum"] != 2 || got[0]["col"] != 18 || got[0]["severity"] != dbDiagWarn {
		t.Fatalf("column diag = %v", got[0])
	}
	if fix, _ := got[0]["user_data"].(map[string]any); fix["fix"] != "name" {
		t.Fatalf("fix = %v", got[0]["user_data"])
	}
	if got[1]["severity"] != dbDiagError {
		t.Fatalf("syntax diag = %v", got[1])
	}
	if got := dbEmbeddedDiagnostics(src, "go", sc, true); len(got) != 1 || got[0]["severity"] != dbDiagWarn {
		t.Fatalf("syntax off = %v", got)
	}
	found := false
	for _, it := range databasesPaletteItems() {
		found = found || it.ID == "db-embedded-check"
	}
	if !found {
		t.Fatal("db-embedded-check not in the palette")
	}
}
