package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-7: saving where the user may not write. The user's text is never
// lost: one clear error toast, the buffer stays modified, the file on
// disk is unchanged, and the quit card still asks about it.

// skipIfRoot skips a permission test when it runs as root (CI
// containers): root writes through every mode bit, so the case cannot
// happen there.
func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("runs as root: file modes do not stop root from writing")
	}
}

// chmodBack sets mode on p and puts it back to restore when the test
// ends, so the temp dirs can be removed.
func chmodBack(t *testing.T, p string, mode, restore os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, restore) })
}

// saveFails presses Ctrl+S on the edited active file and checks the
// promises of SCN-7: an error toast with want in it, the tab still
// modified, the disk text unchanged and the quit card asking.
func saveFails(t *testing.T, d *Driver, rel, disk, want string) {
	t.Helper()
	d.Key("ctrl+s")
	if !d.WaitForText("Save failed", 3*time.Second) {
		t.Fatalf("no Save failed toast:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, want) {
		t.Errorf("the toast does not say %q:\n%s", want, s)
	}
	if got := diskFile(d, rel); got != disk {
		t.Errorf("%s on disk changed to %q", rel, got)
	}
	if got := bufLines(d); !strings.HasPrefix(got, "Hi ") {
		t.Errorf("the editor lost the text: %q", got)
	}
	if !d.WaitFor(func() bool { got, _ := tabs(d); return len(got) > 0 && strings.HasSuffix(got[len(got)-1], "*") }, 3*time.Second) {
		got, _ := tabs(d)
		t.Errorf("the tab is not modified after the failed save: %v", got)
	}
	d.Key("ctrl+q")
	if d.Quit() {
		t.Fatal("quit without asking about the unsaved file")
	}
	if !d.WaitForText("unsaved changes", 3*time.Second) {
		t.Errorf("no unsaved-changes question:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
}

// Spec: Given a.txt is read-only on disk (0444) and edited in the editor,
// when the user saves, then a clear error says it is read-only, the text
// stays modified in the editor and a.txt on disk is unchanged.
func TestSaveReadOnlyFileKeepsText(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "a.txt"), 0o444, 0o644)
	open(d, "a.txt")
	d.Type("Hi ")
	saveFails(t, d, "a.txt", "alpha\nbravo\ncharlie\n", "read-only")
}

// Spec: Given a.txt is read-only on disk and edited,
// when the user runs File: Save from the command palette,
// then the same clear error toast shows as for Ctrl+S.
func TestPaletteSaveReadOnlyFileToasts(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "a.txt"), 0o444, 0o644)
	open(d, "a.txt")
	d.Type("Hi ")
	d.Palette("save")
	if !d.WaitForText("Save failed", 3*time.Second) {
		t.Fatalf("no Save failed toast after File: Save:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "read-only") {
		t.Errorf("the toast does not say read-only:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt on disk changed to %q", got)
	}
}

// Spec: Given a.txt (read-only on disk) and b.txt both edited,
// when the user runs File: Save All Files,
// then b.txt is saved, a.txt is not, a toast names a.txt as not saved
// (no "Saved all" message) and a.txt stays modified.
func TestSaveAllNamesTheFileItCouldNotSave(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "a.txt"), 0o444, 0o644)
	open(d, "a.txt")
	d.Type("Hi ")
	open(d, "b.txt")
	d.Type("Hi ")
	d.Palette("save-all")
	if !d.WaitForText("Save failed", 3*time.Second) {
		t.Fatalf("no Save failed toast after Save All:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, "Could not save a.txt") || strings.Contains(s, "Saved all") {
		t.Errorf("the toast does not name a.txt:\n%s", s)
	}
	if got := diskFile(d, "b.txt"); got != "Hi one\ntwo\nthree\n" {
		t.Errorf("b.txt not saved: %q", got)
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt on disk changed to %q", got)
	}
	if !waitTabs(d, "a.txt*", "b.txt") {
		got, _ := tabs(d)
		t.Errorf("tabs after Save All = %v, want [a.txt* b.txt]", got)
	}
}

// Spec: Given the folder notes is read-only (0555) and a new file
// notes/new.md is typed in the editor,
// when the user saves, then the error says there is no permission to
// write in folder notes, the text stays and no file is made.
func TestSaveNewFileInReadOnlyFolder(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "notes"), 0o555, 0o755)
	d.Action("open:" + filepath.Join(d.Dir, "notes", "new.md"))
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/new.md" }, 5*time.Second) {
		got, _ := tabs(d)
		t.Fatalf("notes/new.md not open: %v", got)
	}
	d.Type("Hi ")
	saveFails(t, d, "notes/new.md", "", "No permission to write in folder notes")
	if exists(d, "notes/new.md") {
		t.Error("a failed save made notes/new.md")
	}
}

// Spec: Given the folder notes is read-only (0555) but notes/todo.md
// itself is writable,
// when the user edits and saves todo.md, then it is saved (a file in a
// locked folder can still be written in place) and nothing is left over
// in the folder.
func TestSaveWritableFileInReadOnlyFolder(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "notes"), 0o555, 0o755)
	open(d, "notes/todo.md")
	d.Type("Hi ")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.HasPrefix(diskFile(d, "notes/todo.md"), "Hi # Todo") }) {
		t.Fatalf("todo.md not saved: %q\n%s", diskFile(d, "notes/todo.md"), screen(d))
	}
	if strings.Contains(screen(d), "Save failed") {
		t.Errorf("a save that worked shows an error:\n%s", screen(d))
	}
	ents, _ := os.ReadDir(filepath.Join(d.Dir, "notes"))
	if len(ents) != 2 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("notes holds %v after the save, want done.md and todo.md", names)
	}
}

// Spec: Given the disk is full (full.txt is swapped for a link to
// /dev/full, which fails every write with ENOSPC) and the buffer is
// edited, when the user saves, then the error says the disk is full and
// the text stays modified in the editor.
func TestSaveOnFullDiskKeepsText(t *testing.T) {
	skipIfRoot(t)
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this system (Linux only)")
	}
	files := textFiles()
	files["full.txt"] = "x\n"
	d := editorDriver(t, files)
	open(d, "full.txt")
	d.Type("Hi ")
	full := filepath.Join(d.Dir, "full.txt")
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/full", full); err != nil {
		t.Fatal(err)
	}
	lua(d, `vim.cmd('silent! checktime'); return ''`)
	d.Key("ctrl+s")
	if !d.WaitForText("Save failed", 3*time.Second) {
		t.Fatalf("no Save failed toast:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "disk is full") {
		t.Errorf("the toast does not say the disk is full:\n%s", screen(d))
	}
	if got := bufLines(d); got != "Hi x" {
		t.Errorf("the editor lost the text: %q", got)
	}
	if !waitTabs(d, "full.txt*") {
		got, _ := tabs(d)
		t.Errorf("not modified after the failed save: %v", got)
	}
}

// Spec: Given a.txt is read-only on disk and edited,
// when the user quits and picks Save All,
// then the app does not quit, the card asks again and says a.txt could
// not be saved, and the text is still in the editor.
func TestQuitSaveAllReadOnlySaysWhy(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, filepath.Join(d.Dir, "a.txt"), 0o444, 0o644)
	open(d, "a.txt")
	d.Type("Hi ")
	waitTabs(d, "a.txt*")
	d.Key("ctrl+q")
	clickButton(d, "Save All")
	if d.Quit() {
		t.Fatal("quit although a.txt could not be saved")
	}
	if !d.WaitForText("Could not save", 3*time.Second) {
		t.Errorf("the card does not say a.txt could not be saved:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("the editor lost the text: %q", got)
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt on disk changed to %q", got)
	}
}
