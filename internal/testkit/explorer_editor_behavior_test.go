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

// Spec: Given a.txt open in a tab,
// when the user renames it to z.txt in the explorer and then saves an edit,
// then the tab shows z.txt, the edit lands in z.txt, and a.txt is not
// written again.
func TestExplorerRenameOpenFileFollowsTab(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	rightClickExplorer(d, "a.txt")
	clickText(d, "Rename...")
	for range len("a.txt") {
		d.Key("backspace")
	}
	d.Type("z.txt")
	d.Key("enter")
	if !waitTabs(d, "z.txt") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the rename: %v", got)
	}
	d.Palette("focus-editor")
	d.Type("X")
	d.Key("ctrl+s")
	d.Settle(300 * time.Millisecond)
	if exists(d, "a.txt") {
		t.Errorf("saving after the rename wrote the old name a.txt again")
	}
	if got := diskFile(d, "z.txt"); got != "Xalpha\nbravo\ncharlie\n" {
		t.Errorf("z.txt = %q, want the edit saved there", got)
	}
}

// Spec: Given notes/todo.md open with unsaved edits,
// when the user renames the folder notes to memo,
// then the tab shows memo/todo.md, keeps the edits, and saving writes
// memo/todo.md.
func TestExplorerRenameFolderOfOpenFile(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "notes/todo.md")
	d.Type("Z")
	rightClickExplorer(d, "notes")
	clickText(d, "Rename...")
	for range len("notes") {
		d.Key("backspace")
	}
	d.Type("memo")
	d.Key("enter")
	if !waitTabs(d, "memo/todo.md*") {
		got, _ := tabs(d)
		t.Fatalf("tab did not follow the folder rename: %v", got)
	}
	d.Palette("focus-editor")
	d.Key("ctrl+s")
	d.Settle(300 * time.Millisecond)
	if got := diskFile(d, "memo/todo.md"); got != "Z# Todo\n\n- milk\n" {
		t.Errorf("memo/todo.md = %q, want the edit saved", got)
	}
	if exists(d, "notes") {
		t.Errorf("saving re-created the old folder notes")
	}
}
