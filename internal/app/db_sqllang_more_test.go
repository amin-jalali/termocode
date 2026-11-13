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

// The SQL string ranges pushed for completion: 0-based rows, byte
// columns of the text (quotes left out), and the quote a name must escape.
func TestDBEmbedRanges(t *testing.T) {
	src := "package x\nvar q = \"SELECT 1 FROM t\"\nvar r = `\nSELECT a\nFROM t`\n"
	got := dbEmbedRanges(src, "go")
	if len(got) != 2 {
		t.Fatalf("ranges = %+v", got)
	}
	if r := got[0]; r.SR != 1 || r.SC != 9 || r.ER != 1 || r.EC != 24 || r.Q != `"` {
		t.Fatalf("first = %+v", r)
	}
	if r := got[1]; r.SR != 2 || r.SC != 9 || r.ER != 4 || r.EC != 6 || r.Q != "" {
		t.Fatalf("raw = %+v", r)
	}
	if !dbSQLAttachable("a/b.py") || !dbSQLAttachable("x.sql") || dbSQLAttachable("x.txt") {
		t.Fatal("attachable files")
	}
}
