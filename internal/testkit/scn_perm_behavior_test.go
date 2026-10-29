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
