package app

import (
	"path/filepath"
	"strings"
	"testing"
)

// Go to Statement lists each statement of the file with its line; the
// title names the detected dialect.
func TestDBSQLOutlineItems(t *testing.T) {
	src := "-- @dialect postgres\nSELECT 1;\n\nUPDATE t SET a = 1 WHERE id = 2;\n"
	dialect, how := dbSQLFileDialect(src, "")
	if dialect != "postgres" || how != "detected" {
		t.Fatalf("dialect = %q %q", dialect, how)
	}
	if d, how := dbSQLFileDialect(src, "mysql"); d != "mysql" || how != "connection" {
		t.Fatalf("connection dialect = %q %q", d, how)
	}
	items := dbSQLOutlineItems(src, dialect)
	if len(items) != 2 || items[0].ID != "2" || items[1].ID != "4" {
		t.Fatalf("items = %+v", items)
	}
	if !strings.Contains(items[1].Title, "UPDATE") {
		t.Fatalf("title = %q", items[1].Title)
	}
	for _, id := range []string{"db-sql-outline", "db-switch-connection"} {
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
}

// The file → connection attachment is saved in the project and read back
// (paths relative to the workspace); a removed connection is dropped.
func TestDBSQLAttachPersisted(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "q", "report.sql")
	if err := dbSaveSQLAttach(root, file, "pg"); err != nil {
		t.Fatal(err)
	}
	got := dbLoadSQLAttach(root)
	if got[file] != "pg" {
		t.Fatalf("loaded = %v", got)
	}
	if err := dbSaveSQLAttach(root, file, ""); err != nil {
		t.Fatal(err)
	}
	if got := dbLoadSQLAttach(root); len(got) != 0 {
		t.Fatalf("detach left %v", got)
	}
}
