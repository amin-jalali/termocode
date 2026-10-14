package testkit

import (
	"testing"
	"time"
)

// Spec (R3): after a turn that changed a file, the chat shows "Undo AI
// changes"; clicking it puts the old text back on disk and in the tab.
func TestAIUndoTurnOnScreen(t *testing.T) {
	_, srv := fakeToolModel(t, toolCall{"write_file", map[string]any{"path": "r.txt", "content": reviewNewA}})
	d := aiDriver(t, srv.URL, reviewFiles(), false)
	open(d, "r.txt")
	askAI(d, "change it")
	if !d.WaitForText("Accept file", 5*time.Second) {
		t.Fatalf("no review card:\n%s", screen(d))
	}
	clickNth(d, "Accept file", 0)
	if !d.WaitForText("Undo AI changes", 5*time.Second) {
		t.Fatalf("no undo button:\n%s", screen(d))
	}
	if diskFile(d, "r.txt") != reviewNewA {
		t.Fatalf("not written: %q", diskFile(d, "r.txt"))
	}
	clickNth(d, "Undo AI changes", 0)
	if !d.WaitFor(func() bool { return diskFile(d, "r.txt") == reviewOldA }, 5*time.Second) {
		t.Fatalf("not restored: %q\n%s", diskFile(d, "r.txt"), screen(d))
	}
	if !eventually(d, func() bool { return bufLines(d)+"\n" == reviewOldA }) {
		t.Errorf("tab %q", bufLines(d))
	}
}
