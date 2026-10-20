package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The add flow is driven through its visible keys. A fake sqlite3 CLI lets
// the test exercise the real connection test without opening a socket.
func TestDatabasesAddSQLiteWithRealKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{
		W: 100, H: 30, Nvim: true,
		Files: map[string]string{"main.go": "package main\n"},
		Setup: func(tb testing.TB, _ string) {
			bin := filepath.Join(t.TempDir(), "bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				tb.Fatal(err)
			}
			fake := `#!/bin/sh
if [ "$1" = "-version" ]; then echo '3.45.1 2024-01-30'; exit 0; fi
while IFS= read -r line; do
  case "$line" in
    SELECT*) echo '[{"ok":1}]' ;;
    .print\ __TC_END_*) echo "${line#*.print }" ;;
  esac
done
`
			if err := os.WriteFile(filepath.Join(bin, "sqlite3"), []byte(fake), 0o755); err != nil {
				tb.Fatal(err)
			}
			tb.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		},
	})

	d.Palette("db-focus")
	d.Key("a")
	if !d.WaitForText("PostgreSQL", 2*time.Second) {
		t.Fatalf("connection type picker did not open:\n%s", screen(d))
	}
	d.Key("down", "down", "enter") // SQLite
	if !d.WaitForText("Database file", 2*time.Second) {
		t.Fatalf("SQLite file prompt did not open:\n%s", screen(d))
	}
	d.Key("enter") // accept data.db
	if !d.WaitForText("Connection works", 5*time.Second) {
		t.Fatalf("connection test did not offer a save choice:\n%s", screen(d))
	}
	d.Key("down", "enter") // Save for me only
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db", "connections.json")
	if !d.WaitFor(func() bool {
		b, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(b), `"id": "data"`)
	}, 5*time.Second) {
		b, _ := os.ReadFile(path)
		t.Fatalf("connection was not saved: %s\n%s", b, screen(d))
	}
}

func TestDatabasesAddPasswordPromptIsMasked(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 100, H: 30, Nvim: true, Files: map[string]string{"main.go": "package main\n"}})
	d.Palette("db-focus")
	d.Key("a", "enter", "enter", "enter", "enter", "enter")
	if !d.WaitForText("Password", 2*time.Second) {
		t.Fatalf("password prompt did not open after the connection fields:\n%s", screen(d))
	}
	d.Type("secret-value-for-test")
	if strings.Contains(screen(d), "secret-value-for-test") {
		t.Fatalf("password appeared in the terminal screen:\n%s", screen(d))
	}
	d.Key("esc")
}
