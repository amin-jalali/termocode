package dbclient

import "testing"

// A name is found as a whole identifier, plain or quoted, in any case;
// strings and comments do not count.
func TestNameUsages(t *testing.T) {
	src := "SELECT o.id FROM orders o -- orders here is a comment\nJOIN \"Orders\" x ON x.id = o.id WHERE note = 'orders' AND orders_old = 1;"
	got := NameUsages(src, "orders")
	if len(got) != 2 {
		t.Fatalf("usages = %+v", got)
	}
	if src[got[0].Start:got[0].End] != "orders" || src[got[1].Start:got[1].End] != "Orders" {
		t.Fatalf("spans = %q %q", src[got[0].Start:got[0].End], src[got[1].Start:got[1].End])
	}
	if n := len(NameUsages(src, "o")); n != 3 {
		t.Fatalf("alias usages = %d", n)
	}
	if NameUsages(src, "") != nil {
		t.Fatal("empty name found usages")
	}
}

// The @dialect line is read alone, without the text hints.
func TestDialectLine(t *testing.T) {
	if d, ok := DialectLine("-- @dialect mysql\nSELECT 1::int;"); !ok || d != DialectMySQL {
		t.Fatalf("dialect line = %q %v", d, ok)
	}
	if _, ok := DialectLine("SELECT 1::int;"); ok {
		t.Fatal("hints counted as a dialect line")
	}
}

// CTE names of a statement (WITH a AS (…), b(x) AS (…)).
func TestCTENames(t *testing.T) {
	got := CTENames("WITH RECURSIVE a AS (SELECT 1), b(x) AS (SELECT 2) SELECT * FROM a JOIN b ON true")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ctes = %v", got)
	}
	if got := CTENames("SELECT a, b FROM t"); len(got) != 0 {
		t.Fatalf("no WITH gave %v", got)
	}
}

// SQL inside Go, Python and JS strings: only literals that read as SQL,
// with their line; comments and plain text strings are left out.
func TestFindEmbeddedSQL(t *testing.T) {
	goSrc := "package x\n// \"SELECT * FROM nope\"\nvar q = `\n  SELECT id\n  FROM users`\nvar s = \"Update available\"\nvar u = \"UPDATE users SET a = $1\"\n"
	got := FindEmbeddedSQL(goSrc, "go")
	if len(got) != 2 || got[0].Line != 3 || got[1].Line != 7 {
		t.Fatalf("go = %+v", got)
	}
	if got[0].Kind != KindRead || got[1].Kind != KindWrite {
		t.Fatalf("kinds = %q %q", got[0].Kind, got[1].Kind)
	}
	py := "# 'DELETE FROM a'\nq = \"\"\"\nINSERT INTO t (a) VALUES (%s)\n\"\"\"\nmsg = 'select a file'\n"
	if got := FindEmbeddedSQL(py, "python"); len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("python = %+v", got)
	}
	js := "const a = 'Drop it';\nconst q = `WITH x AS (SELECT 1) SELECT * FROM x`;\n/* \"DELETE FROM b\" */\n"
	if got := FindEmbeddedSQL(js, "js"); len(got) != 1 || got[0].Line != 2 {
		t.Fatalf("js = %+v", got)
	}
	for path, want := range map[string]string{"a.go": "go", "b.py": "python", "c.tsx": "js", "d.mjs": "js", "e.sql": ""} {
		if got := EmbedLang(path); got != want {
			t.Fatalf("EmbedLang(%s) = %q", path, got)
		}
	}
}

// The SQL in strings gets the same inspections as a SQL file, at its
// place in the code file; a string with backslash escapes is not
// checked (its text is not the SQL the program sends).
func TestInspectEmbedded(t *testing.T) {
	sc := InspectSchema{Tables: map[string]map[string]bool{"": {"users": true}}, Complete: true, Columns: map[string][]string{"users": {"id", "name"}}}
	src := "package x\nvar q = `\n  SELECT id\n  FROM users WHERE (id = 1`\nvar u = \"SELECT u.nme FROM users u\"\n"
	got := InspectEmbedded(src, "go", sc)
	if len(got) != 2 {
		t.Fatalf("issues = %+v", got)
	}
	if got[0].Kind != IssueSyntax || src[got[0].Start:got[0].End] != "(" {
		t.Fatalf("syntax issue = %+v (%q)", got[0], src[got[0].Start:got[0].End])
	}
	if got[1].Kind != IssueColumn || src[got[1].Start:got[1].End] != "nme" || got[1].Fix != "name" {
		t.Fatalf("column issue = %+v (%q)", got[1], src[got[1].Start:got[1].End])
	}
	py := "q = \"\"\"\nSELECT * FROM userz\n\"\"\"\n"
	if got := InspectEmbedded(py, "python", sc); len(got) != 1 || py[got[0].Start:got[0].End] != "userz" {
		t.Fatalf("python = %+v", got)
	}
	js := "const q = 'SELECT \\'it\\' FROM (users';\n"
	if got := InspectEmbedded(js, "js", sc); len(got) != 0 {
		t.Fatalf("escaped string checked: %+v", got)
	}
	if got := InspectEmbedded(src, "", sc); got != nil {
		t.Fatalf("no lang = %+v", got)
	}
}
