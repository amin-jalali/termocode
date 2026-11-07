package dbclient

import (
	"strings"
	"testing"
)

// DBX-46: the schema folder read back as a schema, for completion and for
// a folder-to-database migration script.

func filesMap(files []CanonFile) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		out[f.Name] = f.Text
	}
	return out
}

func withRoutines(s CanonSchema) CanonSchema {
	if s.Driver == "sqlite" {
		return s
	}
	lang := "sql"
	body := "SELECT a + b"
	if s.Driver == "mysql" {
		lang, body = "SQL", "BEGIN\n  RETURN a + b;\nEND"
	}
	s.Routines = []CanonRoutine{{Name: "add", Kind: "FUNCTION", Args: "a integer, b integer", Returns: "integer", Language: lang, Body: body}}
	return s
}

func TestParseSchemaFolderRoundTrip(t *testing.T) {
	for _, driver := range []string{"postgres", "mysql", "sqlite"} {
		src, _ := compareFixture(driver)
		src = withRoutines(src)
		src.Tables[0].Columns = append(src.Tables[0].Columns, CanonColumn{Name: "n", Type: "integer", NotNull: true, Default: "0", Extra: "AUTO_INCREMENT"})
		src.Tables[0].Constraints = append(src.Tables[0].Constraints, CanonConstraint{Kind: "CHECK", Name: "users_n_check", Check: "CHECK ((n >= 0))"})
		files := SchemaObjectFiles(src)
		got, warns := ParseSchemaFolder(driver, "public", filesMap(files))
		if len(warns) != 0 {
			t.Fatalf("%s: warnings %v", driver, warns)
		}
		again := filesMap(SchemaObjectFiles(got))
		for _, f := range files {
			if again[f.Name] != f.Text {
				t.Fatalf("%s: %s does not round-trip:\n%s\n--- got ---\n%s", driver, f.Name, f.Text, again[f.Name])
			}
		}
		if len(again) != len(files) {
			t.Fatalf("%s: %d files back, want %d", driver, len(again), len(files))
		}
	}
}

func TestParseSchemaFolderMigrationMatchesLive(t *testing.T) {
	// Folder → database: the script from the parsed folders is the one the
	// live schemas give.
	for _, driver := range []string{"postgres", "mysql"} {
		src, tgt := compareFixture(driver)
		want := CompareSchemas(src, tgt).Script
		ps, _ := ParseSchemaFolder(driver, src.Schema, filesMap(SchemaObjectFiles(src)))
		pt, _ := ParseSchemaFolder(driver, tgt.Schema, filesMap(SchemaObjectFiles(tgt)))
		if got := CompareSchemas(ps, pt).Script; got != want {
			t.Fatalf("%s script differs:\n%s\n--- want ---\n%s", driver, got, want)
		}
	}
}

func TestParseSchemaFolderHandWritten(t *testing.T) {
	folder := map[string]string{
		"tables/users.sql": "-- users\ncreate table if not exists users (\n  id int primary key,\n  name text not null, -- the name\n  note text default 'a;b'\n);\ncreate unique index users_name on users (name);\n",
		"views/v.sql":      "/* view */\ncreate view v as select id from users;\n",
	}
	s, warns := ParseSchemaFolder("postgres", "public", folder)
	if len(warns) != 0 {
		t.Fatalf("warnings %v", warns)
	}
	if len(s.Tables) != 1 || s.Tables[0].Name != "users" {
		t.Fatalf("tables %+v", s.Tables)
	}
	u := s.Tables[0]
	var cols []string
	for _, c := range u.Columns {
		cols = append(cols, c.Name+":"+c.Type)
	}
	if strings.Join(cols, ",") != "id:int,name:text,note:text" {
		t.Fatalf("columns %v", cols)
	}
	if !u.Columns[1].NotNull || u.Columns[2].Default != "'a;b'" {
		t.Fatalf("column details %+v", u.Columns)
	}
	if len(u.Constraints) != 1 || u.Constraints[0].Kind != "PRIMARY KEY" || u.Constraints[0].Columns[0] != "id" {
		t.Fatalf("inline primary key %+v", u.Constraints)
	}
	if len(u.Indexes) != 1 || !u.Indexes[0].Unique || u.Indexes[0].Name != "users_name" {
		t.Fatalf("index %+v", u.Indexes)
	}
	if len(s.Views) != 1 || s.Views[0].Name != "v" || s.Views[0].Definition != "select id from users" {
		t.Fatalf("views %+v", s.Views)
	}
}

func TestParseSchemaFolderWarnsAndGoesOn(t *testing.T) {
	folder := map[string]string{
		"tables/bad.sql":  "SELECT 1;\n",
		"tables/ok.sql":   "CREATE TABLE \"ok\" (\n  \"a\" text\n);\n",
		"tables/lost.sql": "CREATE INDEX \"i\" ON \"nowhere\" (\"a\");\n",
	}
	s, warns := ParseSchemaFolder("postgres", "public", folder)
	if len(s.Tables) != 1 || s.Tables[0].Name != "ok" {
		t.Fatalf("tables %+v", s.Tables)
	}
	joined := strings.Join(warns, "\n")
	if len(warns) != 2 || !strings.Contains(joined, "tables/bad.sql") || !strings.Contains(joined, "tables/lost.sql") {
		t.Fatalf("warnings %v", warns)
	}
}

func TestCanonCompletion(t *testing.T) {
	src, _ := compareFixture("postgres")
	src = withRoutines(src)
	c := CanonCompletion(src)
	kinds := map[string]string{}
	for _, tb := range c.Tables {
		kinds[tb.Name] = tb.Kind
		if tb.Schema != "public" {
			t.Fatalf("schema of %s = %q", tb.Name, tb.Schema)
		}
	}
	if kinds["users"] != "table" || kinds["active_users"] != "view" || kinds["add"] != "func" {
		t.Fatalf("kinds %v", kinds)
	}
	var names []string
	for _, col := range c.Cols["users"] {
		names = append(names, col.Name+":"+col.Type)
	}
	if strings.Join(names, ",") != "id:bigint,email:text,plan:text" {
		t.Fatalf("users columns %v", names)
	}
	if len(c.FKs) != 1 || c.FKs[0] != (CompletionFK{Table: `odd"name`, Column: "user_id", RefTable: "users", RefColumn: "id"}) {
		t.Fatalf("fks %+v", c.FKs)
	}
}
