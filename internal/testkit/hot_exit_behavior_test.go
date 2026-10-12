package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec: Given a.txt edited (not saved),
// when the user runs Reload Window,
// then the new window shows a.txt with the unsaved text, still modified,
// and asks nothing (a reload is no unclean quit).
func TestReloadKeepsUnsavedText(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	d.Model().PrepareReload() // at once: no time for the 400 ms backup timer
	d.Restart()
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("after reload tabs = %v, want [a.txt*]\n%s", got, screen(d))
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("after reload a.txt holds %q", got)
	}
	d.Settle(200 * time.Millisecond)
	if s := screen(d); strings.Contains(s, "did not quit cleanly") || strings.Contains(s, "Restore unsaved") {
		t.Errorf("reload asked about a crash:\n%s", s)
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("reload wrote the file: %q", got)
	}
}

// Spec: Given a.txt edited (not saved) and its backup written,
// when termocode dies without a clean quit and starts again,
// then it asks to restore; Restore brings the unsaved text back.
func TestUncleanQuitAsksToRestore(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	backedUp := func() bool {
		m, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "termocode", "backup", "*", "*", "*.bak"))
		return len(m) > 0
	}
	if !d.WaitFor(backedUp, 3*time.Second) {
		t.Fatal("a.txt was not backed up")
	}
	d.Restart()
	if !d.WaitForText("did not quit cleanly", 5*time.Second) {
		t.Fatalf("no Restore question after an unclean quit:\n%s", screen(d))
	}
	d.Key("enter") // Restore is the default button
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("after Restore tabs = %v, want [a.txt*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("after Restore a.txt holds %q", got)
	}
}
