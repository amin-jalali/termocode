package dbclient

import (
	"os"
	"path/filepath"
	"testing"
)

// DBX-46: a schema folder (one file per object) read back and compared
// with the live schema both ways, identical items too; a migration tool
// in the project is named.

func TestReadSchemaFolderOnlyObjectFiles(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, text string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("tables/users.sql", "CREATE TABLE users ();\n")
	must("views/v.sql", "CREATE VIEW v AS SELECT 1;\n")
	must("routines/f.sql", "CREATE FUNCTION f();\n")
	must("README.md", "not an object")
	must("tables/notes.txt", "not sql")
	must("other/x.sql", "not a kind folder")
	got, err := ReadSchemaFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got["tables/users.sql"] != "CREATE TABLE users ();\n" || got["views/v.sql"] == "" || got["routines/f.sql"] == "" {
		t.Errorf("folder: %v", got)
	}
	empty, err := ReadSchemaFolder(filepath.Join(dir, "missing"))
	if err != nil || len(empty) != 0 {
		t.Errorf("a missing folder is empty: %v %v", empty, err)
	}
}

func TestCompareSchemaFolderStates(t *testing.T) {
	live := []CanonFile{{Name: "tables/a.sql", Text: "A"}, {Name: "tables/b.sql", Text: "B2"}, {Name: "views/c.sql", Text: "C"}}
	folder := map[string]string{"tables/a.sql": "A", "tables/b.sql": "B1", "routines/old.sql": "OLD"}
	got := CompareSchemaFolder(live, folder)
	want := []SchemaFileDiff{
		{Name: "tables/b.sql", State: SchemaFileChanged, Live: "B2", Folder: "B1"},
		{Name: "views/c.sql", State: SchemaFileLiveOnly, Live: "C"},
		{Name: "routines/old.sql", State: SchemaFileFolderOnly, Folder: "OLD"},
		{Name: "tables/a.sql", State: SchemaFileIdentical, Live: "A", Folder: "A"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestDetectMigrationTool(t *testing.T) {
	cases := map[string]string{
		"flyway.conf":                       "Flyway",
		"liquibase.properties":              "Liquibase",
		"alembic.ini":                       "Alembic",
		"prisma/migrations/x/migration.sql": "Prisma",
		"migrations/0001_init.up.sql":       "golang-migrate",
		"db/migrate/20200101_create.rb":     "Rails",
	}
	for rel, want := range cases {
		dir := t.TempDir()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
		if got := DetectMigrationTool(dir); got != want {
			t.Errorf("%s: got %q want %q", rel, got, want)
		}
	}
	if got := DetectMigrationTool(t.TempDir()); got != "" {
		t.Errorf("empty project: %q", got)
	}
}

func TestApplySchemaFolderLiveToFolder(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "routines"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "routines", "old.sql"), []byte("OLD"), 0o644)
	diffs := []SchemaFileDiff{
		{Name: "views/c.sql", State: SchemaFileLiveOnly, Live: "C"},
		{Name: "routines/old.sql", State: SchemaFileFolderOnly, Folder: "OLD"},
		{Name: "../escape.sql", State: SchemaFileLiveOnly, Live: "X"},
	}
	if _, _, err := ApplySchemaFolder(dir, diffs); err == nil {
		t.Error("a name outside the folder must be refused")
	}
	wrote, removed, err := ApplySchemaFolder(dir, diffs[:2])
	if err != nil || wrote != 1 || removed != 1 {
		t.Fatalf("wrote %d removed %d err %v", wrote, removed, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "views", "c.sql")); string(b) != "C" {
		t.Errorf("views/c.sql = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "routines", "old.sql")); !os.IsNotExist(err) {
		t.Error("routines/old.sql should be removed")
	}
}
