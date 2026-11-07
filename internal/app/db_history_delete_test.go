package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func dbHistoryLines(t *testing.T) []string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "db", "history.jsonl"))
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// The SQL history picker has "Delete an entry…": it lists the entries
// again and the one picked is removed; the others stay.
func TestDBConsoleHistoryDeletesOneEntry(t *testing.T) {
	d, s := dbDriver(t, "-- @conn local\nSELECT 1;\n")
	d.Key("down")
	dbSend(d)
	dbWaitPage(t, d, "row-000")
	d.Key("r")
	if !d.WaitFor(func() bool { return s.count() == 2 && len(dbHistoryLines(t)) == 2 }, 2*time.Second) {
		t.Fatalf("two history entries expected, got %d", len(dbHistoryLines(t)))
	}
	d.Key("h")
	if !d.WaitForText("SQL history", time.Second) {
		t.Fatal("history picker absent")
	}
	d.Type("Delete an entry")
	d.Key("enter")
	if !d.WaitForText("Delete a history entry", time.Second) {
		t.Fatalf("delete picker absent: %s", ansi.Strip(d.Frame()))
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { return len(dbHistoryLines(t)) == 1 }, 2*time.Second) {
		t.Fatalf("history after delete: %v", dbHistoryLines(t))
	}
	if !d.WaitForText("History entry deleted", time.Second) {
		t.Fatalf("no toast: %s", ansi.Strip(d.Frame()))
	}
	if s.count() != 2 {
		t.Fatal("deleting an entry ran it")
	}
}
