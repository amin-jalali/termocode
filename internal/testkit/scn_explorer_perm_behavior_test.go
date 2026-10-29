package testkit

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-7: explorer file operations in a folder the user may not change.

// lockedNotes starts the app on the text workspace with notes expanded
// and then made read-only (0555).
func lockedNotes(t *testing.T) *Driver {
	t.Helper()
	skipIfRoot(t)
	d := New(t, Options{Files: textFiles()})
	clickExplorer(d, "notes")
	if !explorerShows(d, "todo.md") {
		t.Fatalf("notes not expanded:\n%s", screen(d))
	}
	chmodBack(t, filepath.Join(d.Dir, "notes"), 0o555, 0o755)
	return d
}

// plainError waits for an error toast titled title that says in plain
// words that folder notes cannot be changed (no raw "rename /tmp/…:
// permission denied").
func plainError(t *testing.T, d *Driver, title string) {
	t.Helper()
	if !d.WaitForText(title, 3*time.Second) {
		t.Errorf("no %q toast:\n%s", title, screen(d))
		return
	}
	if s := screen(d); !strings.Contains(s, "No permission to change folder notes") {
		t.Errorf("the error is not plain:\n%s", s)
	}
}

// Spec: Given the folder notes is read-only,
// when the user renames notes/todo.md to plan.md, then an error says
// there is no permission, todo.md is still there with its text, and the
// explorer still lists todo.md (not plan.md).
func TestExplorerRenameInReadOnlyFolder(t *testing.T) {
	d := lockedNotes(t)
	rightClickExplorer(d, "todo.md")
	clickText(d, "Rename...")
	for range len("todo.md") {
		d.Key("backspace")
	}
	d.Type("plan.md")
	d.Key("enter")
	plainError(t, d, "Rename failed")
	if got := diskFile(d, "notes/todo.md"); got != "# Todo\n\n- milk\n" || exists(d, "notes/plan.md") {
		t.Errorf("disk changed: todo.md=%q plan.md exists=%v", got, exists(d, "notes/plan.md"))
	}
	if !waitExplorer(d, "todo.md", true) || explorerShows(d, "plan.md") {
		t.Errorf("explorer out of step with the disk:\n%s", screen(d))
	}
}

// Spec: Given the folder notes is read-only,
// when the user deletes notes/todo.md and confirms, then an error says
// there is no permission, todo.md is still on disk and the explorer
// still lists it.
func TestExplorerDeleteInReadOnlyFolder(t *testing.T) {
	d := lockedNotes(t)
	clickExplorer(d, "todo.md")
	d.Key("delete")
	d.Key("enter")
	plainError(t, d, "Delete failed")
	if got := diskFile(d, "notes/todo.md"); got != "# Todo\n\n- milk\n" {
		t.Errorf("todo.md on disk = %q", got)
	}
	if !waitExplorer(d, "todo.md", true) {
		t.Errorf("explorer dropped todo.md although it is still on disk:\n%s", screen(d))
	}
}
