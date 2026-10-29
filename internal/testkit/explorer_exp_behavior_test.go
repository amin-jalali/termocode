package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

func instantExplorerPreview(t *testing.T) {
	t.Helper()
	t.Cleanup(clock.SetAfter(func(d time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- clock.Now(); return ch }))
}
func TestExplorerPreviewPaneKeysAndResize(t *testing.T) {
	instantExplorerPreview(t)
	d := New(t, Options{W: 150, H: 42, Files: map[string]string{"a.txt": "alpha preview", "b.txt": "bravo preview"}})
	d.Action("view:files")
	d.Palette("focus-explorer")
	d.Key("p")
	d.Key("down")
	if !strings.Contains(screen(d), "alpha preview") {
		t.Fatal(screen(d))
	}
	if bufs, _ := d.Model().OpenBuffers(); len(bufs) != 0 {
		t.Fatal("preview opened editor", bufs)
	}
	d.Key("down")
	if !strings.Contains(screen(d), "bravo preview") {
		t.Fatal(screen(d))
	}
	d.Resize(99, 30)
	if !strings.Contains(screen(d), "100 cols") {
		t.Fatal(screen(d))
	}
	d.Resize(150, 42)
	if !strings.Contains(screen(d), "bravo preview") {
		t.Fatal(screen(d))
	}
	d.Key("p")
	if strings.Contains(screen(d), "bravo preview") {
		t.Fatal("toggle did not hide preview")
	}
	d.Palette("explorer-toggle-preview")
	if !strings.Contains(screen(d), "bravo preview") {
		t.Fatal("palette toggle")
	}
}
func TestExplorerBulkRenameKeysConflictAndUndo(t *testing.T) {
	d := New(t, Options{W: 150, H: 42, Files: map[string]string{"a.txt": "alpha", "b.txt": "bravo"}})
	d.Action("view:files")
	d.Palette("focus-explorer")
	d.Key("down", "ctrl+@", "shift+down", "r")
	if !strings.Contains(screen(d), "One name per line") {
		t.Fatal(screen(d))
	}
	// Replace each full line without removing the line separator.
	d.Key("ctrl+k")
	d.Type("c.txt")
	d.Key("down", "home", "ctrl+k")
	d.Type("c.txt")
	d.Key("ctrl+s")
	if !strings.Contains(screen(d), "Conflict:") {
		t.Fatal(screen(d))
	}
	d.Key("enter")
	if diskFile(d, "a.txt") != "alpha" {
		t.Fatal("default applied conflict")
	}
	d.Palette("explorer-rename-selected")
	d.Key("ctrl+k")
	d.Type("b.txt")
	d.Key("down", "home", "ctrl+k")
	d.Type("a.txt")
	d.Key("ctrl+s")
	if !strings.Contains(screen(d), "a.txt → b.txt") {
		t.Fatal(screen(d))
	}
	d.Key("tab", "enter")
	if diskFile(d, "a.txt") != "bravo" || diskFile(d, "b.txt") != "alpha" {
		t.Fatal("swap failed", screen(d))
	}
	if !strings.Contains(screen(d), "Renamed 2 files") {
		t.Fatal("missing undo toast", screen(d))
	}
	clickText(d, "Renamed 2 files")
	if diskFile(d, "a.txt") != "alpha" || diskFile(d, "b.txt") != "bravo" {
		t.Fatal("undo failed")
	}
}
