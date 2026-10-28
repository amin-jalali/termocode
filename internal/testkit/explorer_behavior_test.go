package testkit

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Spec: Given a workspace,
// when the user right-clicks the root, picks New File... and types a
// nested name "src/app/main.txt",
// then the missing folders are made and the file exists.
func TestExplorerNewFileMakesParents(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "workspace")
	clickText(d, "New File...")
	d.Type("src/app/main.txt")
	d.Key("enter")
	if !eventually(d, func() bool { return exists(d, "src/app/main.txt") }) {
		t.Fatal("src/app/main.txt not created")
	}
	if !explorerShows(d, "src") {
		t.Errorf("explorer does not list the new folder src:\n%s", screen(d))
	}
}

// Spec: Given a file a.txt,
// when the user picks New File... and types the existing name "a.txt",
// then the file keeps its content (no overwrite) and an error is shown.
func TestExplorerNewFileExistingNameKeepsContent(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "workspace")
	clickText(d, "New File...")
	d.Type("a.txt")
	d.Key("enter")
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Fatalf("a.txt changed: %q", got)
	}
}

// Spec: Given a workspace,
// when the user right-clicks the root, picks New Folder... and types "lib",
// then the folder lib exists and the explorer lists it.
func TestExplorerNewFolder(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "workspace")
	clickText(d, "New Folder...")
	d.Type("lib")
	d.Key("enter")
	eventually(d, func() bool { return exists(d, "lib") })
	if fi, err := os.Stat(filepath.Join(d.Dir, "lib")); err != nil || !fi.IsDir() {
		t.Fatalf("lib is not a folder: %v", err)
	}
	if !explorerShows(d, "lib") {
		t.Errorf("explorer does not list lib:\n%s", screen(d))
	}
}

// Spec: Given files a.txt and b.txt,
// when the user renames a.txt to "b.txt",
// then nothing is overwritten: both files keep their content and an
// error is shown.
func TestExplorerRenameOntoExistingFileRefused(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Rename...")
	for range len("a.txt") {
		d.Key("backspace")
	}
	d.Type("b.txt")
	d.Key("enter")
	d.Settle(200 * time.Millisecond)
	if a, b := diskFile(d, "a.txt"), diskFile(d, "b.txt"); a != "alpha\nbravo\ncharlie\n" || b != "one\ntwo\nthree\n" {
		t.Fatalf("rename onto b.txt: a.txt=%q b.txt=%q, want both unchanged", a, b)
	}
	if _, _, ok := find(d, "exists"); !ok {
		t.Errorf("no error shown:\n%s", screen(d))
	}
}

// Spec: Given a file a.txt,
// when the user right-clicks it, picks Rename... and replaces the name
// with "z.txt",
// then z.txt has the old content, a.txt is gone, and the explorer lists
// z.txt instead of a.txt.
func TestExplorerRenameFile(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	rightClickExplorer(d, "a.txt")
	clickText(d, "Rename...")
	for range len("a.txt") {
		d.Key("backspace")
	}
	d.Type("z.txt")
	d.Key("enter")
	if !eventually(d, func() bool { return !exists(d, "a.txt") && diskFile(d, "z.txt") == "alpha\nbravo\ncharlie\n" }) {
		t.Fatalf("rename failed: a.txt exists=%v z.txt=%q", exists(d, "a.txt"), diskFile(d, "z.txt"))
	}
	if explorerShows(d, "a.txt") || !explorerShows(d, "z.txt") {
		t.Errorf("explorer not updated:\n%s", screen(d))
	}
}

// Spec: Given a file b.txt under the explorer cursor,
// when the user presses Delete and confirms with Enter,
// then b.txt is removed from disk and from the explorer.
// And when the user presses Delete on c.txt and cancels with Esc,
// then c.txt stays.
func TestExplorerDeleteConfirm(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	clickExplorer(d, "b.txt")
	d.Key("delete")
	if !slices.ContainsFunc([]string{"Delete?", "permanently delete"}, func(s string) bool {
		_, _, ok := find(d, s)
		return ok
	}) {
		t.Fatalf("no confirm dialog:\n%s", screen(d))
	}
	if !exists(d, "b.txt") {
		t.Fatal("b.txt deleted before the user confirmed")
	}
	d.Key("enter")
	if !eventually(d, func() bool { return !exists(d, "b.txt") }) {
		t.Fatal("b.txt not deleted after confirm")
	}
	if explorerShows(d, "b.txt") {
		t.Errorf("explorer still lists b.txt:\n%s", screen(d))
	}

	clickExplorer(d, "c.txt")
	d.Key("delete")
	d.Key("esc")
	if !exists(d, "c.txt") {
		t.Fatal("c.txt deleted although the user cancelled")
	}
}

// Spec: Given two files selected with Shift+Down,
// when the user presses Delete and confirms,
// then both files are removed and the third one stays.
func TestExplorerBulkDelete(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	clickExplorer(d, "a.txt")
	d.Key("shift+down")
	d.Key("delete")
	if _, _, ok := find(d, "Delete 2"); !ok {
		t.Fatalf("bulk confirm does not say 2 items:\n%s", screen(d))
	}
	d.Key("enter")
	if !eventually(d, func() bool { return !exists(d, "a.txt") && !exists(d, "b.txt") }) {
		t.Fatalf("selected files not deleted: a=%v b=%v", exists(d, "a.txt"), exists(d, "b.txt"))
	}
	if !exists(d, "c.txt") {
		t.Fatal("c.txt deleted but was not selected")
	}
}

// Spec: Given the folder notes collapsed,
// when the user clicks it, then its files are listed;
// when the user clicks it again, then they are hidden.
func TestExplorerFolderExpandCollapse(t *testing.T) {
	d := New(t, Options{Files: textFiles()})
	if explorerShows(d, "todo.md") {
		t.Fatal("notes starts expanded")
	}
	clickExplorer(d, "notes")
	if !explorerShows(d, "todo.md") || !explorerShows(d, "done.md") {
		t.Fatalf("notes did not expand:\n%s", screen(d))
	}
	clickExplorer(d, "notes")
	if explorerShows(d, "todo.md") {
		t.Fatalf("notes did not collapse:\n%s", screen(d))
	}
}

// Spec: Given a file ".env" in the workspace,
// when the explorer starts, then it is hidden;
// when the user runs Toggle Hidden Files, then it is listed.
func TestExplorerToggleHiddenFiles(t *testing.T) {
	files := textFiles()
	files[".env"] = "SECRET=1\n"
	d := New(t, Options{Files: files})
	if explorerShows(d, ".env") {
		t.Fatal(".env shown before toggling hidden files")
	}
	d.Palette("toggle-hidden")
	if !explorerShows(d, ".env") {
		t.Fatalf(".env not shown after toggling hidden files:\n%s", screen(d))
	}
}

// Spec: Given a second folder outside the workspace,
// when the user runs Add Folder to Workspace and types its path,
// then the explorer lists it as a second root with its files;
// and when the user runs Remove Folder and picks it, then it is gone.
func TestExplorerMultiRootAddRemove(t *testing.T) {
	// The prompt's and the picker's Enter come back as async messages; a
	// tiny settle window makes the test wait for them, not race them.
	d := New(t, Options{Files: textFiles(), Quiet: time.Nanosecond})
	other := filepath.Join(filepath.Dir(d.Dir), "otherproj")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(other, "extra.txt"), []byte("x\n"), 0o644)

	d.Palette("workspace-add-folder")
	// The prompt starts with the home folder; replace it.
	for range len(os.Getenv("HOME")) + 1 {
		d.Key("backspace")
	}
	d.Type(other)
	d.Key("enter")
	if !waitExplorer(d, "otherproj", true) {
		t.Fatalf("second root not listed:\n%s", screen(d))
	}
	clickExplorer(d, "otherproj")
	if !explorerShows(d, "extra.txt") {
		// The root may start expanded; a click then collapsed it.
		clickExplorer(d, "otherproj")
	}
	if !explorerShows(d, "extra.txt") {
		t.Fatalf("second root's files not listed:\n%s", screen(d))
	}

	d.Palette("workspace-remove-folder")
	d.Type("otherproj")
	d.Key("enter")
	if !waitExplorer(d, "otherproj", false) {
		t.Fatalf("second root still listed after remove:\n%s", screen(d))
	}
}

// waitExplorer waits until the explorer lists name (want) or no longer
// lists it (!want), re-drawing the frame on each try.
func waitExplorer(d *Driver, name string, want bool) bool {
	d.T.Helper()
	return d.WaitFor(func() bool { d.render(); return explorerShows(d, name) == want }, 5*time.Second)
}
