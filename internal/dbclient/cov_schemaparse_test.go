package dbclient

import (
	"strings"
	"testing"
)

func TestParseSchemaFolderWarnsAboutWhatItCannotRead(t *testing.T) {
	files := map[string]string{
		"a.sql": `
-- a comment
SELECT 1;
CREATE TEMP TABLE tmp (x int);
CREATE OR REPLACE VIEW ignored AS SELECT 1;
CREATE TABLE;
CREATE TABLE nolist;
CREATE TABLE bad (a int, ???);
CREATE TABLE twice (a int);
CREATE TABLE twice (b int);
CREATE INDEX;
CREATE INDEX i1;
CREATE INDEX i2 ON;
CREATE INDEX i3 ON t;
CREATE INDEX ix_orphan ON missing (a);
CREATE TRIGGER x AFTER INSERT ON t BEGIN SELECT 1; END;
CREATE SEQUENCE s;
CREATE VIEW;
CREATE FUNCTION;
CREATE PROCEDURE;
`,
		"views/v.sql":     "CREATE VIEW v NOAS SELECT 1",
		"views/empty.sql": "   ",
		"z.sql":           "CREATE INDEX i4 ON t (a",
	}
	_, warns := ParseSchemaFolder("postgresql", "public", files)
	all := strings.Join(warns, "\n")
	for _, want := range []string{
		"not a CREATE statement", "a CREATE TABLE without a name", "nolist has no column list",
		"table bad: cannot read", "table twice is in the folder twice", "a CREATE INDEX without a name",
		"index i1 has no ON table", "index i2 has no table", "index i3 has no column list", "index i4 has no column list",
		"ix_orphan is on table missing", "cannot read CREATE TRIGGER", "cannot read CREATE SEQUENCE",
		"a CREATE VIEW without a name", "view v has no AS",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no warning with %q in:\n%s", want, all)
		}
	}
}

func TestParseSchemaFolderConstraintsIndexesAndViewsForms(t *testing.T) {
	sch, warns := ParseSchemaFolder("mysql", "app", map[string]string{
		"t.sql": "CREATE TABLE parent (id int PRIMARY KEY, code varchar(10) NOT NULL DEFAULT 'x' AUTO_INCREMENT);\n" +
			"CREATE TABLE child (\n id int,\n pid int,\n CONSTRAINT fk_p FOREIGN KEY (pid) REFERENCES parent (id) ON UPDATE CASCADE ON DELETE SET NULL,\n" +
			" CONSTRAINT uq UNIQUE KEY (id),\n CHECK (id > 0),\n PRIMARY KEY (id),\n FOREIGN KEY (pid) REFERENCES parent\n);\n" +
			"CREATE UNIQUE INDEX ux ON child (id);\n" +
			"CREATE FULLTEXT INDEX ft ON child (pid);\n" +
			"CREATE INDEX ix ON child (pid) USING btree WHERE pid > 1;\n" +
			"CREATE INDEX IF NOT EXISTS ix2 ON child (pid) USING hash;\n",
		"views/m.sql":    "CREATE MATERIALIZED VIEW mv AS\nSELECT 1",
		"routines/f.sql": "CREATE FUNCTION f(a integer) RETURNS integer LANGUAGE sql AS $$ SELECT a $$",
	})
	_ = warns
	if len(sch.Tables) != 2 {
		t.Fatalf("tables = %+v", sch.Tables)
	}
	child := sch.Tables[1]
	var kinds []string
	for _, c := range child.Constraints {
		kinds = append(kinds, c.Kind)
	}
	for _, want := range []string{"FOREIGN KEY", "UNIQUE", "CHECK", "PRIMARY KEY"} {
		if !strings.Contains(strings.Join(kinds, ","), want) {
			t.Errorf("child constraints %v lack %s", kinds, want)
		}
	}
	fk := child.Constraints[0]
	if fk.OnUpdate != "CASCADE" || fk.OnDelete != "SET NULL" || fk.RefTable != "parent" || len(fk.RefColumns) != 1 {
		t.Errorf("fk = %+v", fk)
	}
	if len(child.Indexes) != 4 {
		t.Errorf("indexes = %+v", child.Indexes)
	}
	if len(sch.Views) != 1 || !sch.Views[0].Materialized || strings.HasPrefix(sch.Views[0].Definition, "\n") {
		t.Errorf("views = %+v", sch.Views)
	}
	parent := sch.Tables[0]
	if len(parent.Constraints) != 1 || parent.Columns[1].Default != "'x'" || !parent.Columns[1].NotNull || parent.Columns[1].Extra != "AUTO_INCREMENT" {
		t.Errorf("parent = %+v", parent)
	}
	sq, _ := ParseSchemaFolder("sqlite", "", map[string]string{
		"t.sql": "CREATE TABLE t (a int);\nCREATE INDEX i1 ON t (\"a\");\nCREATE INDEX i2 ON t (lower(a));\nCREATE INDEX i3 ON t (\"a\") WHERE a > 1;\nCREATE VIEW v AS SELECT 1;",
	})
	raws := 0
	for _, ix := range sq.Tables[0].Indexes {
		if ix.Raw != "" {
			raws++
		}
	}
	if raws != 2 || len(sq.Views) != 1 || !sq.Views[0].Full {
		t.Errorf("sqlite raw indexes = %d, views = %+v", raws, sq.Views)
	}
}

func TestSchemaFolderMigrationScriptAndReviewNotes(t *testing.T) {
	diffs := []SchemaFileDiff{
		{Name: "tables.sql", Folder: "CREATE TABLE a (id int, extra text);\nSELECT 1;", Live: "CREATE TABLE a (id int);"},
		{Name: "new.sql", Folder: "CREATE TABLE b (id int);"},
		{Name: "gone.sql", Live: "CREATE TABLE c (id int);"},
	}
	script := SchemaFolderMigration("postgres", "public", "my folder", diffs)
	if !strings.Contains(script, "-- REVIEW: tables.sql: not a CREATE statement") {
		t.Errorf("a line that cannot be read comes first as a note:\n%s", script)
	}
	if !strings.Contains(script, "extra") || !strings.Contains(script, "b") {
		t.Errorf("the script adds what the folder has:\n%s", script)
	}
	if !strings.Contains(strings.ToUpper(script), "DROP TABLE") {
		t.Errorf("the script drops what only the database has:\n%s", script)
	}
	comp := CanonCompletion(CanonSchema{Schema: "s",
		Tables: []CanonTable{{Name: "T", Columns: []CanonColumn{{Name: "c", Type: "int"}}, Constraints: []CanonConstraint{
			{Kind: "FOREIGN KEY", Columns: []string{"c", "d"}, RefTable: "U", RefColumns: []string{"id"}}, {Kind: "UNIQUE"}}}},
		Views: []CanonView{{Name: "v"}}, Routines: []CanonRoutine{{Name: "f"}}})
	if len(comp.Tables) != 3 || len(comp.FKs) != 2 || comp.FKs[1].RefColumn != "" || comp.Cols["t"][0].Type != "int" {
		t.Errorf("completion = %+v", comp)
	}
}
