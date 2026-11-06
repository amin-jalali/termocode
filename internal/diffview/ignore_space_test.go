package diffview

import (
	"strings"
	"testing"
)

// GITX-26 (C7): w hides whitespace-only changes in a read-only card (and
// shows them again); the hint bar says when they are hidden. A review
// card keeps every change (its hunks are applied to the file).
func TestIgnoreWhitespaceKey(t *testing.T) {
	files := []File{{Name: "x.go", Old: "a\n\tb\nc\nd\ne\n", New: "a\n    b\nc\nd\nE\n"}}
	m := New("Compare", files, false)
	m.SetSize(120, 30)
	if n := len(m.files[0].diff.Hunks); n != 2 {
		t.Fatalf("hunks %d", n)
	}
	m, _ = key(m, "w")
	if !m.IgnoreSpace() || len(m.files[0].diff.Hunks) != 1 {
		t.Fatalf("w: ignore %v hunks %d", m.IgnoreSpace(), len(m.files[0].diff.Hunks))
	}
	if !strings.Contains(plain(m), "whitespace hidden") {
		t.Errorf("no note:\n%s", plain(m))
	}
	// A file loaded later (commit view) follows the toggle.
	c := NewCommit(Header{Hash: "abc"}, []File{{Name: "y.go", Pending: true}}, nil)
	c.SetSize(120, 30)
	c, _ = key(c, "w")
	c.SetFileText(0, "\tx\n", "  x\n")
	if c.files[0].diff.HasChanges() {
		t.Error("a later file shows a whitespace-only change")
	}
	m, _ = key(m, "w")
	if m.IgnoreSpace() || len(m.files[0].diff.Hunks) != 2 {
		t.Fatal("w again did not show them")
	}
	r := New("Review", files, true)
	r.SetSize(120, 30)
	r, _ = key(r, "w")
	if r.IgnoreSpace() {
		t.Error("a review card hid changes it applies")
	}
}
