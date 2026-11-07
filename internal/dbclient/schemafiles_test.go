package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaObjectFilesOnePerObject(t *testing.T) {
	src, _ := compareFixture("postgres")
	src.Routines = []CanonRoutine{
		{Name: "add", Kind: "FUNCTION", Args: "a integer, b integer", Returns: "integer", Language: "sql", Body: "SELECT a + b"},
		{Name: "add", Kind: "FUNCTION", Args: "a numeric", Returns: "numeric", Language: "sql", Body: "SELECT a"},
	}
	files := SchemaObjectFiles(src)
	var names []string
	byName := map[string]string{}
	for _, f := range files {
		names = append(names, f.Name)
		byName[f.Name] = f.Text
	}
	want := []string{"tables/odd_name.sql", "tables/users.sql", "views/active_users.sql", "routines/add.sql", "routines/add-2.sql"}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("names %q\nwant %q", names, want)
	}
	if tb := byName["tables/odd_name.sql"]; !strings.Contains(tb, `CREATE TABLE "odd""name"`) || !strings.Contains(tb, "orders_user_idx") {
		t.Fatalf("table file holds its DDL and indexes:\n%s", tb)
	}
	if !strings.Contains(byName["views/active_users.sql"], "active_users") {
		t.Fatalf("view file:\n%s", byName["views/active_users.sql"])
	}
	// Stable: the same schema in another order gives the same files.
	sh := src
	sh.Tables = []CanonTable{src.Tables[1], src.Tables[0]}
	again := SchemaObjectFiles(sh)
	for i := range files {
		if files[i] != again[i] {
			t.Fatalf("not stable at %d", i)
		}
	}
}

func TestSchemaObjectFilesSafeNames(t *testing.T) {
	s := CanonSchema{Driver: "sqlite", Tables: []CanonTable{
		{Name: "../etc/passwd", Columns: []CanonColumn{{Name: "a", Type: "text"}}},
		{Name: "Users", Columns: []CanonColumn{{Name: "a", Type: "text"}}},
		{Name: "users", Columns: []CanonColumn{{Name: "a", Type: "text"}}},
		{Name: "", Columns: []CanonColumn{{Name: "a", Type: "text"}}},
	}}
	seen := map[string]bool{}
	for _, f := range SchemaObjectFiles(s) {
		base := strings.TrimPrefix(f.Name, "tables/")
		if strings.ContainsAny(base, `/\`) || strings.HasPrefix(base, ".") || base == ".sql" {
			t.Fatalf("unsafe name %q", f.Name)
		}
		if seen[strings.ToLower(f.Name)] {
			t.Fatalf("names collide on a case-blind disk: %q", f.Name)
		}
		seen[strings.ToLower(f.Name)] = true
	}
	if len(seen) != 4 {
		t.Fatalf("%d files", len(seen))
	}
}

func TestWriteSchemaFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "schema")
	src, _ := compareFixture("postgres")
	files := SchemaObjectFiles(src)
	n, err := WriteSchemaFolder(dir, files)
	if err != nil || n != len(files) {
		t.Fatalf("wrote %d of %d: %v", n, len(files), err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "tables", "users.sql"))
	if err != nil || !strings.Contains(string(b), "CREATE TABLE") {
		t.Fatalf("users.sql: %q %v", b, err)
	}
	// A second dump with the same schema writes nothing new.
	if n, err := WriteSchemaFolder(dir, files); err != nil || n != 0 {
		t.Fatalf("unchanged dump wrote %d: %v", n, err)
	}
	// A name that leaves the folder is refused.
	if _, err := WriteSchemaFolder(dir, []CanonFile{{Name: "../x.sql", Text: "x"}}); err == nil {
		t.Fatal("wrote outside the folder")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "x.sql")); err == nil {
		t.Fatal("file outside the folder exists")
	}
}
