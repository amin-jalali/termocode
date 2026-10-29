package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Spec: Given a.txt edited (not saved) and its backup written,
// when termocode crashes (a panic in the UI loop: the report is marked)
// and starts again,
// then the unsaved text is back at once, with no Restore question, and a
// card says termocode stopped unexpectedly and the work was restored.
func TestCrashRestoresUnsavedText(t *testing.T) {
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
	report, err := crash.WriteReport("update", "test crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := crash.MarkFatal(report); err != nil {
		t.Fatal(err)
	}
	d.Restart()
	if !d.WaitForText("stopped unexpectedly", 5*time.Second) {
		t.Fatalf("no crash card:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "work was restored") {
		t.Errorf("card does not say the work was restored:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Restore unsaved") {
		t.Errorf("asked to restore after a crash:\n%s", screen(d))
	}
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v, want [a.txt*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("a.txt holds %q", got)
	}
	d.Key("enter") // Close
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "stopped unexpectedly") }, 3*time.Second) {
		t.Fatalf("Enter did not close the card:\n%s", screen(d))
	}
	if crash.Pending() != "" {
		t.Error("the crash is still pending")
	}
}

// Spec: Given a.txt edited (not saved) and backed up, then termocode
// crashes and a.txt is changed on disk by someone else,
// when termocode starts again (the unsaved text comes back at once),
// then the user is told a.txt changed on disk (Close first), the disk
// keeps the new text, and the crash card follows.
func TestCrashRestoreChecksTheDisk(t *testing.T) {
	d := editorDriver(t, nil)
	crashWithBackup(t, d)
	if err := os.WriteFile(filepath.Join(d.Dir, "a.txt"), []byte("NEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := crash.WriteReport("update", "test crash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := crash.MarkFatal(report); err != nil {
		t.Fatal(err)
	}
	d.Restart()
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v, want [a.txt*]", got)
	}
	if got := bufLines(d); got != "Hi alpha\nbravo\ncharlie" {
		t.Errorf("a.txt holds %q", got)
	}
	if !d.WaitForText("changed on disk", 5*time.Second) {
		t.Fatalf("not told that a.txt changed on disk:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "NEW\n" {
		t.Errorf("the new disk text was overwritten: %q", got)
	}
	d.Key("enter") // Close
	if !d.WaitForText("stopped unexpectedly", 5*time.Second) {
		t.Fatalf("no crash card after the disk card:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "NEW\n" {
		t.Errorf("closing the card wrote a.txt: %q", got)
	}
}
