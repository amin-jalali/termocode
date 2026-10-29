package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-9: hot exit when the disk changed before the restart. The user's
// unsaved text always comes back and they are told the disk changed; the
// new disk text is never overwritten without a word.

// crashWithBackup edits a.txt, waits for its backup and returns: the
// test then changes the disk and calls restoreAfterCrash.
func crashWithBackup(t *testing.T, d *Driver) {
	t.Helper()
	open(d, "a.txt")
	d.Type("Hi ")
	backedUp := func() bool {
		m, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "termocode", "backup", "*", "*", "*.bak"))
		return len(m) > 0
	}
	if !d.WaitFor(backedUp, 3*time.Second) {
		t.Fatal("a.txt was not backed up")
	}
}

// restoreAfterCrash starts the app again and answers Restore.
func restoreAfterCrash(t *testing.T, d *Driver) {
	t.Helper()
	d.Restart()
	if !d.WaitForText("did not quit cleanly", 5*time.Second) {
		t.Fatalf("no Restore question after an unclean quit:\n%s", screen(d))
	}
	d.Key("enter") // Restore
}

// Spec: Given a.txt edited (not saved) and backed up, then termocode
// dies and a.txt is changed on disk by someone else,
// when termocode starts again and the user picks Restore,
// then the unsaved text is back (modified), the disk keeps the new text,
// and a card says a.txt changed on disk with Close first and Compare.
func TestHotExitRestoreAfterDiskChanged(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	if err := os.WriteFile(filepath.Join(d.Dir, "a.txt"), []byte("NEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreAfterCrash(t, d)
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("after Restore tabs = %v, want [a.txt*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("after Restore a.txt holds %q", got)
	}
	if !d.WaitForText("changed on disk", 3*time.Second) {
		t.Fatalf("not told that a.txt changed on disk:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "NEW\n" {
		t.Errorf("the new disk text was overwritten: %q", got)
	}
	// Close is first and focused: Enter only closes the card.
	d.Key("enter")
	if strings.Contains(screen(d), "changed on disk") {
		t.Errorf("Enter did not close the card:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "NEW\n" {
		t.Errorf("closing the card wrote a.txt: %q", got)
	}
}

// Spec: Given the restored a.txt changed on disk, when the user picks
// Compare on the card, then a diff shows the disk text next to the
// restored text, and nothing is written.
func TestHotExitDiskChangedCompare(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	if err := os.WriteFile(filepath.Join(d.Dir, "a.txt"), []byte("NEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreAfterCrash(t, d)
	if !d.WaitForText("changed on disk", 3*time.Second) {
		t.Fatalf("no card:\n%s", screen(d))
	}
	d.Key("tab", "enter") // Close is focused; Compare is next
	if !d.WaitForText("Disk changed", 3*time.Second) {
		t.Fatalf("no diff card:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, "NEW") || !strings.Contains(s, "Hi alpha") {
		t.Errorf("the diff does not show both texts:\n%s", s)
	}
	if got := diskFile(d, "a.txt"); got != "NEW\n" {
		t.Errorf("Compare wrote a.txt: %q", got)
	}
}

// Spec: Given a.txt unchanged on disk since its backup, when the user
// restores, then no disk-changed card shows.
func TestHotExitRestoreSameDiskAsksNothing(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	restoreAfterCrash(t, d)
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("after Restore tabs = %v", got)
	}
	d.Settle(300 * time.Millisecond)
	if s := screen(d); strings.Contains(s, "changed on disk") {
		t.Errorf("a card for a file that did not change:\n%s", s)
	}
}

// Spec: Given a.txt backed up, then deleted on disk before the restart,
// when the user restores, then the text is back (modified) and a warning
// says a.txt was deleted on disk; nothing is written until the user saves.
func TestHotExitRestoreAfterDiskDeleted(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	if err := os.Remove(filepath.Join(d.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	restoreAfterCrash(t, d)
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("after Restore tabs = %v, want [a.txt*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("after Restore a.txt holds %q", got)
	}
	if !d.WaitForText("deleted on disk", 3*time.Second) {
		t.Errorf("not told that a.txt was deleted:\n%s", screen(d))
	}
	if exists(d, "a.txt") {
		t.Errorf("restore wrote a.txt again without a save")
	}
}

// Spec: Given a.txt backed up, then replaced on disk by a folder a.txt,
// when the user restores, then the text comes back in a new tab
// a.txt.restored (modified), the folder is untouched and a warning says
// where the text is.
func TestHotExitRestoreWhenFileBecameFolder(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	p := filepath.Join(d.Dir, "a.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	restoreAfterCrash(t, d)
	if !waitTabs(d, "a.txt.restored*") {
		got, _ := tabs(d)
		t.Fatalf("after Restore tabs = %v, want [a.txt.restored*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("restored text = %q", got)
	}
	if !d.WaitForText("now a folder", 3*time.Second) {
		t.Errorf("not told that a.txt is a folder now:\n%s", screen(d))
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() || !exists(d, "a.txt/inner") {
		t.Errorf("the folder a.txt was changed")
	}
}
