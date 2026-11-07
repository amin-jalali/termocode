package dbclient

import (
	"strings"
	"testing"
)

func TestObjectSourceQueryPerDriver(t *testing.T) {
	cases := []struct {
		driver string
		kind   ObjectKind
		want   string
		col    int
		args   int
	}{
		{"postgres", ObjectView, "pg_get_viewdef", 0, 1},
		{"postgres", ObjectFunction, "pg_get_functiondef", 0, 1},
		{"postgres", ObjectProcedure, "pg_get_functiondef", 0, 1},
		{"postgres", ObjectTrigger, "pg_get_triggerdef", 0, 1},
		{"mysql", ObjectView, "SHOW CREATE VIEW `v1`", 1, 0},
		{"mariadb", ObjectFunction, "SHOW CREATE FUNCTION `v1`", 2, 0},
		{"mysql", ObjectProcedure, "SHOW CREATE PROCEDURE `v1`", 2, 0},
		{"mysql", ObjectTrigger, "SHOW CREATE TRIGGER `v1`", 2, 0},
		{"sqlite", ObjectView, "sqlite_master", 0, 2},
		{"sqlite", ObjectTrigger, "sqlite_master", 0, 2},
	}
	for _, c := range cases {
		q, err := ObjectSourceQuery(c.driver, c.kind, "v1")
		if err != nil {
			t.Fatalf("%s %s: %v", c.driver, c.kind, err)
		}
		if !strings.Contains(q.SQL, c.want) || q.Column != c.col || len(q.Args) != c.args {
			t.Fatalf("%s %s: got %+v", c.driver, c.kind, q)
		}
	}
}

func TestObjectSourceQueryRejects(t *testing.T) {
	if _, err := ObjectSourceQuery("sqlite", ObjectFunction, "f"); err == nil {
		t.Fatal("sqlite has no stored functions; want an error")
	}
	if _, err := ObjectSourceQuery("redis", ObjectView, "v"); err == nil {
		t.Fatal("redis has no views; want an error")
	}
	if _, err := ObjectSourceQuery("postgres", ObjectView, " "); err == nil {
		t.Fatal("empty name; want an error")
	}
}

func TestObjectSourceQueryQuotesMySQLName(t *testing.T) {
	q, err := ObjectSourceQuery("mysql", ObjectView, "a`b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.SQL, "`a``b`") {
		t.Fatalf("name not quoted: %s", q.SQL)
	}
}

func TestSubmitObjectSourcePostgresUsesCreateOrReplace(t *testing.T) {
	p, err := SubmitObjectSource("postgres", ObjectView, "v1", "CREATE VIEW v1 AS SELECT 1;")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 1 || !strings.HasPrefix(p.Statements[0], "CREATE OR REPLACE VIEW v1") || !p.Transactional {
		t.Fatalf("got %+v", p)
	}
	p, err = SubmitObjectSource("postgres", ObjectTrigger, "t1", "create trigger t1 after insert on x for each row execute function f()")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Statements[0], "CREATE OR REPLACE trigger t1") {
		t.Fatalf("got %q", p.Statements[0])
	}
	// already CREATE OR REPLACE: kept as is
	src := "CREATE OR REPLACE FUNCTION public.f1() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$"
	p, _ = SubmitObjectSource("postgres", ObjectFunction, "f1", src)
	if p.Statements[0] != src {
		t.Fatalf("changed a CREATE OR REPLACE source: %q", p.Statements[0])
	}
}

func TestSubmitObjectSourceDropsFirstWhereNeeded(t *testing.T) {
	p, err := SubmitObjectSource("sqlite", ObjectTrigger, "t1", "CREATE TRIGGER t1 AFTER INSERT ON x BEGIN SELECT 1; END;")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 2 || p.Statements[0] != `DROP TRIGGER IF EXISTS "t1"` || !p.Transactional {
		t.Fatalf("got %+v", p)
	}
	p, err = SubmitObjectSource("mysql", ObjectFunction, "f1", "CREATE FUNCTION f1() RETURNS INT RETURN 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Statements) != 2 || p.Statements[0] != "DROP FUNCTION IF EXISTS `f1`" || p.Transactional {
		t.Fatalf("got %+v", p)
	}
	p, _ = SubmitObjectSource("mysql", ObjectView, "v1", "CREATE ALGORITHM=UNDEFINED VIEW `v1` AS select 1")
	if len(p.Statements) != 1 || !strings.HasPrefix(p.Statements[0], "CREATE OR REPLACE ALGORITHM") {
		t.Fatalf("got %+v", p)
	}
}

func TestSubmitObjectSourceRefusesUnsafeSource(t *testing.T) {
	bad := map[string]string{
		"empty":      "  ",
		"not create": "DROP TABLE users",
		"other name": "CREATE VIEW other AS SELECT 1",
		"wrong kind": "CREATE FUNCTION v1() RETURNS int AS 'select 1' LANGUAGE sql",
		"two stmts":  "CREATE VIEW v1 AS SELECT 1; DROP TABLE users;",
	}
	for name, src := range bad {
		if _, err := SubmitObjectSource("postgres", ObjectView, "v1", src); err == nil {
			t.Errorf("%s: want an error for %q", name, src)
		}
	}
	// a function body with ';' inside dollar quotes is one statement
	if _, err := SubmitObjectSource("postgres", ObjectFunction, "f1", "CREATE FUNCTION f1() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;"); err != nil {
		t.Fatalf("dollar-quoted body refused: %v", err)
	}
}

func TestSourceEditDirtyDiffOutdated(t *testing.T) {
	e := NewSourceEdit("conn1", ObjectView, "v1", "CREATE VIEW v1 AS\nSELECT 1;\n")
	if e.Dirty() {
		t.Fatal("new edit is dirty")
	}
	e.Text = "CREATE VIEW v1 AS\nSELECT 2;\n"
	if !e.Dirty() {
		t.Fatal("changed edit not dirty")
	}
	d := e.Diff()
	if d.Added != 1 || d.Removed != 1 {
		t.Fatalf("diff: +%d -%d", d.Added, d.Removed)
	}
	prev := e.Preview()
	if !strings.Contains(prev, "-SELECT 1;") || !strings.Contains(prev, "+SELECT 2;") {
		t.Fatalf("preview:\n%s", prev)
	}
	// stored text unchanged except line endings / trailing space: not outdated
	if e.Outdated("CREATE VIEW v1 AS\r\nSELECT 1;  \r\n") {
		t.Fatal("line endings made it outdated")
	}
	if !e.Outdated("CREATE VIEW v1 AS\nSELECT 3;\n") {
		t.Fatal("changed stored source not outdated")
	}
}

func TestPendingSourcesList(t *testing.T) {
	var p PendingSources
	a := NewSourceEdit("c", ObjectView, "b_view", "x")
	b := NewSourceEdit("c", ObjectFunction, "a_fn", "y")
	p.Put(a)
	p.Put(b)
	if p.Len() != 0 {
		t.Fatal("clean edits counted as pending")
	}
	a.Text, b.Text = "x2", "y2"
	p.Put(a)
	p.Put(b)
	got := p.List()
	if len(got) != 2 || got[0].Name != "a_fn" || got[1].Name != "b_view" {
		t.Fatalf("list: %+v", got)
	}
	a.Text = "x"
	p.Put(a) // back to stored: drops out
	if p.Len() != 1 {
		t.Fatalf("len %d", p.Len())
	}
	p.Drop("c", ObjectFunction, "a_fn")
	if p.Len() != 0 {
		t.Fatal("drop failed")
	}
}
