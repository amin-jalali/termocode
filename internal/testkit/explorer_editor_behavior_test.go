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

// Spec: Given a workspace with a folder "notes",
// when the user right-clicks it, picks New File... and types "idea.md",
// then notes/idea.md exists (empty) and opens as the active tab.
func TestExplorerNewFileInFolder(t *testing.T) {
	d := editorDriver(t, nil)
	rightClickExplorer(d, "notes")
	clickText(d, "New File...")
	d.Type("idea.md")
	d.Key("enter")
	if !exists(d, "notes/idea.md") || diskFile(d, "notes/idea.md") != "" {
		t.Fatalf("notes/idea.md not created empty")
	}
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/idea.md" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("new file not the active tab: active %q, tabs %v", a, got)
	}
}

// Spec: Given notes/todo.md open while the folder notes is collapsed,
// when the user runs Reveal File (Ctrl+Shift+E),
// then notes is expanded and the explorer cursor is on todo.md.
func TestExplorerRevealActiveFile(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "notes/todo.md")
	// Collapse notes again (opening may have revealed it).
	if explorerShows(d, "todo.md") {
		clickExplorer(d, "notes")
	}
	d.Palette("reveal-file")
	if !explorerShows(d, "todo.md") {
		t.Fatalf("todo.md not revealed:\n%s", screen(d))
	}
	if cur, _ := d.Model().ExplorerCursor(); relPath(d, cur) != "notes/todo.md" {
		t.Errorf("explorer cursor on %q, want notes/todo.md", relPath(d, cur))
	}
}

// Spec: Given a file whose name has a space and an accent ("my café.txt"),
// when the user clicks it in the explorer,
// then it opens as the active tab with its content.
func TestExplorerOpenFileWithSpaceInName(t *testing.T) {
	files := textFiles()
	files["my café.txt"] = "latte\n"
	d := editorDriver(t, files)
	clickExplorer(d, "my café.txt")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "my café.txt" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("active tab %q (tabs %v), want my café.txt", a, got)
	}
	if got := bufLines(d); got != "latte" {
		t.Errorf("buffer = %q, want latte", got)
	}
}

// Spec: Given a.txt open and files named "100%.txt" and "#1.txt" (Vim
// reads % and # in a file name as "current file" / "alternate file"),
// when the user opens each from the explorer, from Quick Open (Ctrl+P)
// and with Open to the Side,
// then that exact file opens with its content.
func TestOpenFileWithVimSpecialCharsInName(t *testing.T) {
	files := textFiles()
	files["100%.txt"] = "pct\n"
	files["#1.txt"] = "hash\n"
	d := editorDriver(t, files)
	open(d, "a.txt")
	check := func(how, rel, body string) {
		t.Helper()
		if !d.WaitFor(func() bool { _, a := tabs(d); return a == rel }, 3*time.Second) {
			got, a := tabs(d)
			t.Fatalf("%s %s: active %q (tabs %v)", how, rel, a, got)
		}
		if got := bufLines(d); got != body {
			t.Errorf("%s %s: buffer = %q, want %q", how, rel, got, body)
		}
	}

	clickExplorer(d, "100%.txt")
	check("explorer", "100%.txt", "pct")

	open(d, "a.txt")
	d.Key("ctrl+p")
	d.Type("#1")
	if !d.WaitForText("#1.txt", 3*time.Second) {
		t.Fatalf("quick open does not list #1.txt:\n%s", screen(d))
	}
	d.Key("enter")
	check("quick open", "#1.txt", "hash")

	open(d, "a.txt")
	rightClickExplorer(d, "100%.txt")
	clickText(d, "Open to the Side")
	check("open to the side", "100%.txt", "pct")
}
