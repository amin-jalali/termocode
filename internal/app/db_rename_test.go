package app

import (
	"os"
	"path/filepath"
	"testing"
)

// DBX-5: rename replaces whole names only, keeps quotes and leaves string
// literals and comments alone.
func TestDBRenameText(t *testing.T) {
	in := "SELECT u.id FROM users u JOIN \"Users\" x ON 1=1 -- users here\nWHERE note = 'users' AND users_old = 1 AND public.users.id > 0;"
	got, n := dbRenameText(in, "users", "customers")
	want := "SELECT u.id FROM customers u JOIN \"customers\" x ON 1=1 -- users here\nWHERE note = 'users' AND users_old = 1 AND public.customers.id > 0;"
	if got != want || n != 3 {
		t.Fatalf("got %d:\n%s", n, got)
	}
	if _, n := dbRenameText("SELECT 1", "users", "x"); n != 0 {
		t.Fatalf("count %d", n)
	}
	got, _ = dbRenameText("SELECT `users`.id FROM `users`", "users", "a b")
	if got != "SELECT `a b`.id FROM `a b`" {
		t.Fatalf("backquotes: %s", got)
	}
	got, _ = dbRenameText("SELECT * FROM users", "users", "Order Items")
	if got != `SELECT * FROM "Order Items"` {
		t.Fatalf("a name that needs quotes: %s", got)
	}
}

func TestDBRenameFilesSkipsCurrentAndHidden(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		p = filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.sql", "SELECT * FROM users;\nSELECT 1 FROM users u;")
	write("b.sql", "SELECT 1;")
	write("cur.sql", "SELECT * FROM users;")
	write(".hidden/c.sql", "SELECT * FROM users;")
	write("notes.txt", "users")
	got := dbRenameFiles(root, filepath.Join(root, "cur.sql"), "users", "customers")
	if len(got) != 1 || filepath.Base(got[0].path) != "a.sql" || got[0].count != 2 || got[0].text != "SELECT * FROM customers;\nSELECT 1 FROM customers u;" {
		t.Fatalf("files: %+v", got)
	}
}

// DBX-5: go to definition of alias.column resolves the alias in the
// statement under the cursor.
func TestDBStatementAliases(t *testing.T) {
	a := dbStatementAliases(`SELECT o.total FROM public.orders AS o JOIN users u ON u.id = o.user_id JOIN "Order Items" oi ON true`)
	if a["o"] != "orders" || a["u"] != "users" || a["users"] != "users" || a["oi"] != "Order Items" {
		t.Fatalf("aliases: %v", a)
	}
	if a2 := dbStatementAliases("SELECT * FROM users WHERE 1=1"); a2["where"] != "" || a2["users"] != "users" {
		t.Fatalf("keyword taken as alias: %v", a2)
	}
}

func TestDBRenamePaletteAndPolicy(t *testing.T) {
	found := false
	for _, it := range databasesPaletteItems() {
		if it.ID == "db-rename" {
			found = it.Title == "Databases: Rename in SQL…"
		}
	}
	if !found {
		t.Fatal("no Databases: Rename in SQL… palette item")
	}
	if _, ok := aiActionPolicies["db-rename"]; !ok {
		t.Fatal("db-rename has no AI policy")
	}
}
