package testkit

import (
	"testing"
	"time"
)

// Explorer behaviour that needs the editor (a real nvim).

// Spec: Given b.txt open in a tab,
// when the user deletes b.txt from the explorer,
// then its tab closes (no editor keeps a deleted file).
func TestExplorerDeleteOpenFileClosesTab(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	d.Palette("focus-explorer")
	clickExplorer(d, "b.txt")
	d.Key("delete")
	d.Key("enter")
	if exists(d, "b.txt") {
		t.Fatal("b.txt not deleted")
	}
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Errorf("tabs after delete = %v, want [a.txt]", got)
	}
}

// Spec: Given b.txt open with unsaved edits,
// when the user deletes b.txt from the explorer and confirms,
// then the file stays deleted (closing the tab does not save it back).
func TestExplorerDeleteModifiedOpenFile(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	d.Type("X")
	if !waitTabs(d, "a.txt", "b.txt*") {
		got, _ := tabs(d)
		t.Fatalf("b.txt not marked modified: %v", got)
	}
	d.Palette("focus-explorer")
	clickExplorer(d, "b.txt")
	d.Key("delete")
	d.Key("enter")
	d.Settle(300 * time.Millisecond)
	if exists(d, "b.txt") {
		t.Fatalf("b.txt exists after delete (content %q)", diskFile(d, "b.txt"))
	}
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Errorf("tabs after delete = %v, want [a.txt]", got)
	}
}
