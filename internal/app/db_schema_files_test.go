package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-46: F on a schema dumps it to a folder or compares it with one.

func TestDBSchemaFolderPath(t *testing.T) {
	if got := dbSchemaFolderPath("/work", "schema/local"); got != "/work/schema/local" {
		t.Errorf("relative: %s", got)
	}
	if got := dbSchemaFolderPath("/work", "/abs/dir"); got != "/abs/dir" {
		t.Errorf("absolute: %s", got)
	}
	if got := dbSchemaFolderDefault(dbCompareSide{conn: "my pg", schema: "public"}); got != "schema/my_pg-public" {
		t.Errorf("default: %s", got)
	}
}

func TestDBSchemaCompareItems(t *testing.T) {
	diffs := []dbclient.SchemaFileDiff{
		{Name: "tables/b.sql", State: dbclient.SchemaFileChanged},
		{Name: "views/c.sql", State: dbclient.SchemaFileLiveOnly},
		{Name: "routines/old.sql", State: dbclient.SchemaFileFolderOnly},
		{Name: "tables/a.sql", State: dbclient.SchemaFileIdentical},
	}
	items := dbSchemaCompareItems(diffs, map[string]bool{"tables/b.sql": true})
	var heads, rows []string
	for _, it := range items {
		if it.Header {
			heads = append(heads, it.Title)
		} else {
			rows = append(rows, it.ID+"="+it.Title)
		}
	}
	if strings.Join(heads, "|") != "Changed|Only in the database|Only in the folder|Identical" {
		t.Errorf("headers: %v", heads)
	}
	if len(rows) != 4 || !strings.HasPrefix(rows[0], "tables/b.sql=[x] ") || !strings.HasPrefix(rows[1], "views/c.sql=[ ] ") {
		t.Errorf("rows: %v", rows)
	}
	if strings.Contains(rows[3], "[") {
		t.Errorf("an identical row cannot be selected: %s", rows[3])
	}
}

// A DDL folder is a completion source: its tables and columns join the
// payload, the live schema keeps what it already has.
func TestDBSchemaPayloadUsesDDLFolder(t *testing.T) {
	s := &dbState{nodes: map[string]dbNode{}}
	s.setFullSchema("local", dbclient.CompletionSchema{
		Tables: []dbclient.CompletionTable{{Schema: "public", Name: "users", Kind: "table"}},
		Cols:   map[string][]dbclient.Column{"users": {{Name: "id", Type: "integer"}}},
	})
	folder, warns := dbclient.ParseSchemaFolder("postgres", "public", map[string]string{
		"tables/users.sql":  "CREATE TABLE \"users\" (\n  \"id\" integer,\n  \"email\" text\n);\n",
		"tables/orders.sql": "CREATE TABLE \"orders\" (\n  \"id\" bigint,\n  \"user_id\" integer,\n  CONSTRAINT \"o_fk\" FOREIGN KEY (\"user_id\") REFERENCES \"users\" (\"id\")\n);\n",
	})
	if len(warns) != 0 {
		t.Fatalf("warnings %v", warns)
	}
	before := s.schemaVer
	s.setDDLSchema("local", dbclient.CanonCompletion(folder))
	if s.schemaVer == before {
		t.Fatal("schema version not bumped")
	}
	p := s.schemaPayload(dbclient.Conn{ID: "local", Driver: "postgres"})
	names := map[string]bool{}
	for _, tb := range p["tables"].([]map[string]any) {
		names[tb["n"].(string)] = true
	}
	if !names["users"] || !names["orders"] {
		t.Fatalf("tables %v", names)
	}
	cols := p["cols"].(map[string][]string)
	if strings.Join(cols["users"], ",") != "id" || strings.Join(cols["orders"], ",") != "id,user_id" {
		t.Fatalf("cols %v (the live users columns win, orders comes from the folder)", cols)
	}
	if fks := p["fks"].([]map[string]any); len(fks) != 1 || fks[0]["t"] != "orders" {
		t.Fatalf("fks %v", fks)
	}
	s.resetConnectionTree("local")
	if s.ddl["local"] == nil {
		t.Fatal("a reconnect dropped the folder source; the folder did not change")
	}
}

// Folder → database: the selected rows become an ALTER/CREATE/DROP script.
func TestDBSchemaFolderMigrationScript(t *testing.T) {
	live := "CREATE TABLE \"users\" (\n  \"id\" integer\n);\n"
	folder := "CREATE TABLE \"users\" (\n  \"id\" integer,\n  \"email\" text\n);\n"
	script := dbclient.SchemaFolderMigration("postgres", "public", "schema/pg", []dbclient.SchemaFileDiff{
		{Name: "tables/users.sql", State: dbclient.SchemaFileChanged, Live: live, Folder: folder},
		{Name: "tables/new.sql", State: dbclient.SchemaFileFolderOnly, Folder: "CREATE TABLE \"new\" (\n  \"a\" text\n);\n"},
		{Name: "tables/old.sql", State: dbclient.SchemaFileLiveOnly, Live: "CREATE TABLE \"old\" (\n  \"a\" text\n);\n"},
		{Name: "tables/bad.sql", State: dbclient.SchemaFileFolderOnly, Folder: "DROP TABLE x;\n"},
	})
	for _, want := range []string{
		`ALTER TABLE "public"."users" ADD COLUMN "email" text;`,
		`CREATE TABLE "public"."new"`,
		`DROP TABLE "public"."old"`,
		"-- REVIEW: tables/bad.sql",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script lacks %q:\n%s", want, script)
		}
	}
}
