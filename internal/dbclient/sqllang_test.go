package dbclient

import (
	"reflect"
	"testing"
)

// DBX-41: dialect of a SQL file from a `-- @dialect` line or its text.
func TestDetectDialect(t *testing.T) {
	cases := []struct {
		src  string
		want SQLDialect
		ok   bool
	}{
		{"-- @dialect mysql\nSELECT 1;", DialectMySQL, true},
		{"-- a note\n--  @dialect  PostgreSQL\nSELECT 1;", DialectPostgres, true},
		{"-- @dialect sqlite3\n", DialectSQLite, true},
		{"CREATE TABLE t (id INT AUTO_INCREMENT PRIMARY KEY) ENGINE=InnoDB;", DialectMySQL, true},
		{"SELECT * FROM `users` LIMIT 1;", DialectMySQL, true},
		{"CREATE TABLE t (id SERIAL PRIMARY KEY); SELECT x::text FROM t WHERE n ILIKE 'a%' RETURNING id;", DialectPostgres, true},
		{"CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;", DialectPostgres, true},
		{"PRAGMA foreign_keys = ON; CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT);", DialectSQLite, true},
		{"SELECT 1;", "", false},
		{"-- @dialect oracle\nSELECT 1;", "", false},
		// Text inside strings and comments does not count.
		{"SELECT 'ENGINE=InnoDB', 'x::int'; -- AUTO_INCREMENT", "", false},
	}
	for _, c := range cases {
		got, ok := DetectDialect(c.src)
		if got != c.want || ok != c.ok {
			t.Errorf("%q → %q %v, want %q %v", c.src, got, ok, c.want, c.ok)
		}
	}
}

// DBX-41: the statement outline (line, kind, short label) of a file.
func TestOutline(t *testing.T) {
	src := "-- setup\nCREATE TABLE users (\n  id int\n);\n\nINSERT INTO users VALUES (1);\nSELECT *\n  FROM users\n  WHERE id = 1;\n" +
		"SELECT 'a;b' AS semi FROM orders;\n" + "DELETE FROM users WHERE id = 2"
	got := Outline(src, "postgres")
	want := []OutlineItem{
		{Line: 2, Kind: KindDDL, Label: "CREATE TABLE users ("},
		{Line: 6, Kind: KindWrite, Label: "INSERT INTO users VALUES (1)"},
		{Line: 7, Kind: KindRead, Label: "SELECT * FROM users WHERE id = 1"},
		{Line: 10, Kind: KindRead, Label: "SELECT 'a;b' AS semi FROM orders"},
		{Line: 11, Kind: KindWrite, Label: "DELETE FROM users WHERE id = 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%+v\nwant\n%+v", got, want)
	}
	long := "SELECT " + string(make([]byte, 0)) + "aaaaaaaaaa, bbbbbbbbbb, cccccccccc, dddddddddd, eeeeeeeeee, ffffffffff, gggggggggg FROM t;"
	if l := Outline(long, "postgres")[0].Label; len([]rune(l)) > outlineLabelMax || l[len(l)-len("…"):] != "…" {
		t.Fatalf("long label %q", l)
	}
	if Outline("  \n-- only a comment\n", "postgres") != nil {
		t.Fatal("empty file has an outline")
	}
}
