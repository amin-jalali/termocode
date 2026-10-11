package testkit

import (
	"strings"
	"testing"
	"time"
)

// Spec: Given a.txt and b.txt open, b.txt active with the cursor on line 3,
// and the folder notes expanded,
// when the user quits and starts termocode again in the same folder,
// then both tabs come back, b.txt is active with the cursor on line 3,
// and notes is still expanded.
func TestSessionRestoreFilesAndCursor(t *testing.T) {
	d := editorDriver(t, nil)
	clickExplorer(d, "notes")
	open(d, "a.txt")
	open(d, "b.txt")
	d.Key("down", "down")
	if !d.WaitFor(func() bool { return cursor(d) == "3:1" }, 2*time.Second) {
		t.Fatalf("cursor at %s before restart", cursor(d))
	}
	d.Settle(300 * time.Millisecond) // let the session save the cursor

	d.Restart()
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("restored tabs = %v, want [a.txt b.txt]", got)
	}
	if _, a := tabs(d); a != "b.txt" {
		t.Errorf("restored active tab %q, want b.txt", a)
	}
	if got := cursor(d); !strings.HasPrefix(got, "3:") {
		t.Errorf("restored cursor at %s, want line 3", got)
	}
	if !explorerShows(d, "todo.md") {
		t.Errorf("notes not expanded after restart:\n%s", screen(d))
	}
}
