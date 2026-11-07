package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SCN-18: drag and drop in the explorer. A drop on a folder (or on a file:
// its folder) asks first; Cancel has the focus, Move moves.

// dragExplorer drags the explorer row from onto the row onto.
func dragExplorer(d *Driver, from, onto string) {
	d.T.Helper()
	y0, ok := explorerRow(d, from)
	if !ok {
		d.T.Fatalf("explorer does not show %q:\n%s", from, screen(d))
	}
	y1, ok := explorerRow(d, onto)
	if !ok {
		d.T.Fatalf("explorer does not show %q:\n%s", onto, screen(d))
	}
	d.Drag(10, y0, 10, y1)
}

// Spec: Given a.txt and the folder notes,
// when the user drags a.txt onto notes and picks Move,
// then a.txt is in notes with its text and gone from the top.
func TestSCN18DragFileIntoFolder(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	dragExplorer(d, "a.txt", "notes")
	if !strings.Contains(screen(d), "Move") || !strings.Contains(screen(d), "Cancel") {
		t.Fatalf("no move card:\n%s", screen(d))
	}
	if _, err := os.Stat(filepath.Join(d.Dir, "a.txt")); err != nil {
		t.Fatalf("a.txt moved before Move was picked: %v", err)
	}
	clickButton(d, "Move")
	if !eventually(d, func() bool { _, err := os.Stat(filepath.Join(d.Dir, "notes", "a.txt")); return err == nil }) {
		t.Fatalf("notes/a.txt not made:\n%s", screen(d))
	}
	if got := diskFile(d, "notes/a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("notes/a.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(d.Dir, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("a.txt is still at the top: %v", err)
	}
}

// Spec: Given a.txt and the folder notes,
// when the user drags a.txt onto notes and presses Enter,
// then Cancel was picked and nothing moved.
func TestSCN18DragEnterCancels(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	dragExplorer(d, "a.txt", "notes")
	d.Key("enter")
	if _, err := os.Stat(filepath.Join(d.Dir, "a.txt")); err != nil {
		t.Errorf("a.txt is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.Dir, "notes", "a.txt")); !os.IsNotExist(err) {
		t.Errorf("notes/a.txt made after Cancel: %v", err)
	}
}

// Spec: Given the closed folder notes,
// when the user drags notes onto the row below it (the press opens notes,
// so that row is one of its own files),
// then no card opens, the app says why and notes stays.
func TestSCN18DragFolderIntoItselfRefused(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	y, ok := explorerRow(d, "notes")
	if !ok {
		t.Fatalf("explorer does not show notes:\n%s", screen(d))
	}
	d.Drag(10, y, 10, y+1)
	if !strings.Contains(screen(d), "cannot move into itself") {
		t.Errorf("no reason shown:\n%s", screen(d))
	}
	if got := diskFile(d, "notes/todo.md"); got != "# Todo\n\n- milk\n" {
		t.Errorf("notes/todo.md = %q", got)
	}
}
