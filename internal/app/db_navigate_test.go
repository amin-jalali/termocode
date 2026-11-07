package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDBIdentAt(t *testing.T) {
	for _, tc := range []struct {
		line         string
		col          int
		schema, name string
	}{
		{"SELECT * FROM users WHERE id = 1", 15, "", "users"},
		{"SELECT * FROM users WHERE id = 1", 18, "", "users"},
		{"SELECT * FROM public.users u", 22, "public", "users"},
		{"SELECT * FROM public.users u", 15, "public", "users"},
		{`SELECT * FROM "Order Items"`, 17, "", "Order Items"},
		{"SELECT * FROM `shop`.`orders`", 24, "shop", "orders"},
		{"SELECT * FROM users", 7, "", ""}, // on the "*"
		{"", 0, "", ""},
		{"x", 9, "", ""},
	} {
		s, n := dbIdentAt(tc.line, tc.col)
		if s != tc.schema || n != tc.name {
			t.Errorf("%q @%d = %q.%q, want %q.%q", tc.line, tc.col, s, n, tc.schema, tc.name)
		}
	}
}

func TestDBFindTablePath(t *testing.T) {
	s := newDBState()
	s.nodes["c/db/app/schema/public/table/users"] = dbNode{kind: dbNodeTable, name: "users", conn: "c"}
	s.nodes["c/db/app/schema/audit/table/users"] = dbNode{kind: dbNodeTable, name: "users", conn: "c"}
	s.nodes["c/db/app/schema/public/view/paying"] = dbNode{kind: dbNodeView, name: "paying", conn: "c"}
	s.nodes["other/db/x/schema/public/table/orders"] = dbNode{kind: dbNodeTable, name: "orders", conn: "other"}
	s.nodes["c/db/app/schema/public/table/users/col/users"] = dbNode{kind: dbNodeColumn, name: "users", conn: "c"}
	for _, tc := range []struct{ conn, schema, name, want string }{
		{"c", "audit", "USERS", "c/db/app/schema/audit/table/users"},
		{"c", "", "users", "c/db/app/schema/audit/table/users"}, // first by path, stable
		{"c", "", "Paying", "c/db/app/schema/public/view/paying"},
		{"c", "", "orders", ""}, // another connection's table
		{"c", "nope", "users", ""},
	} {
		if got := s.dbFindTablePath(tc.conn, tc.schema, tc.name); got != tc.want {
			t.Errorf("%s %q.%q = %q, want %q", tc.conn, tc.schema, tc.name, got, tc.want)
		}
	}
}

func TestDBFindUsages(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("q/a.sql", "SELECT * FROM users;\nSELECT * FROM users_old;\nselect id from USERS u\n")
	write("b.SQL", "-- users\n")
	write("notes.txt", "users\n")
	write("node_modules/x.sql", "users\n")
	write(".git/y.sql", "users\n")
	got := dbFindUsages(root, "users", 100)
	want := []dbUsage{
		{path: filepath.Join(root, "b.SQL"), line: 1, text: "-- users"},
		{path: filepath.Join(root, "q/a.sql"), line: 1, text: "SELECT * FROM users;"},
		{path: filepath.Join(root, "q/a.sql"), line: 3, text: "select id from USERS u"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := dbFindUsages(root, "users", 2); len(got) != 2 {
		t.Fatalf("limit: %d", len(got))
	}
}
