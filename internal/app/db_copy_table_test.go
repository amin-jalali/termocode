package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDBCopyParseName(t *testing.T) {
	for _, c := range []struct{ in, schema, name string }{
		{"users", "", "users"},
		{" public.users ", "public", "users"},
		{"app.my.table", "app", "my.table"},
	} {
		s, n, err := dbCopyParseName(c.in)
		if err != nil || s != c.schema || n != c.name {
			t.Fatalf("%q: %q %q %v", c.in, s, n, err)
		}
	}
	for _, bad := range []string{"", "  ", ".users", "public."} {
		if _, _, err := dbCopyParseName(bad); err == nil {
			t.Fatalf("%q was taken", bad)
		}
	}
}

func TestDBCopyDefaultName(t *testing.T) {
	src := dbTable{conn: "a", driver: "postgres", sessKey: "a", schema: "public", name: "users"}
	if got := dbCopyDefaultName(src, dbCopyTarget{sessKey: "a", driver: "postgres"}); got != "" {
		t.Fatalf("same session: %q", got)
	}
	if got := dbCopyDefaultName(src, dbCopyTarget{sessKey: "b", driver: "postgres"}); got != "public.users" {
		t.Fatalf("same kind: %q", got)
	}
	if got := dbCopyDefaultName(src, dbCopyTarget{sessKey: "c", driver: "mysql"}); got != "users" {
		t.Fatalf("other kind: %q", got)
	}
	if got := dbCopyDefaultName(dbTable{sessKey: "s", driver: "sqlite", schema: "main", name: "t"}, dbCopyTarget{sessKey: "s2", driver: "sqlite"}); got != "t" {
		t.Fatalf("sqlite: %q", got)
	}
}

func TestDBCopySweepRemovesOnlyOldCopyFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("copy-1.csv", 48*time.Hour)
	fresh := write("copy-2.csv", time.Minute)
	other := write("notes.txt", 48*time.Hour)
	dbCopySweep(dir, now)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("an old copy file stayed")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s was removed", p)
		}
	}
}

func TestDBImportEndRemovesTheCopyFileOnly(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "copy-1.csv")
	picked := filepath.Join(dir, "data.csv")
	for _, p := range []string{tmp, picked} {
		if err := os.WriteFile(p, []byte("id\n1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := &dbConsoleState{imp: &dbImport{path: tmp, tmp: tmp}}
	s.dbImportEnd()
	if s.imp != nil {
		t.Fatal("the import stayed")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("the copy file stayed")
	}
	s.imp = &dbImport{path: picked}
	s.dbImportEnd()
	if _, err := os.Stat(picked); err != nil {
		t.Fatal("a file the user picked was removed")
	}
	s.dbImportEnd() // nothing to end: no panic
}
