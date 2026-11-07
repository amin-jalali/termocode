package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DBX-50: `termocode db` lists, shows, adds and removes the user's
// connections without the UI; a literal password never prints.
func TestDBCommand(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "termocode", "db", "connections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"connections":[{"id":"pg","name":"Shop","driver":"postgres","host":"db","port":5432,"password":"hunter2"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runDBIn(args, strings.NewReader(stdin), &out, &errw)
		return code, out.String(), errw.String()
	}
	if code, out, _ := run("", "list"); code != 0 || !strings.Contains(out, "pg") || !strings.Contains(out, "postgres") || !strings.Contains(out, "db:5432") {
		t.Fatalf("list %d %q", code, out)
	}
	if code, out, _ := run("", "show", "pg"); code != 0 || !strings.Contains(out, `"host": "db"`) || strings.Contains(out, "hunter2") {
		t.Fatalf("show %d %q", code, out)
	}
	if code, _, _ := run(`{"id":"my","driver":"mysql","host":"m","port":3306}`, "add"); code != 0 {
		t.Fatal("add failed")
	}
	if code, out, _ := run("", "list"); code != 0 || !strings.Contains(out, "my") {
		t.Fatalf("after add %q", out)
	}
	if code, _, _ := run("", "remove", "pg"); code != 0 {
		t.Fatal("remove failed")
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), `"pg"`) || !strings.Contains(string(b), `"my"`) {
		t.Fatalf("file after remove: %s", b)
	}
	if code, _, errw := run("", "remove", "nope"); code != 1 || !strings.Contains(errw, "nope") {
		t.Fatalf("remove unknown %d %q", code, errw)
	}
	if code, _, _ := run("", "bogus"); code != 2 {
		t.Fatal("usage error not 2")
	}
}
