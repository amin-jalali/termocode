package confirm

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A command in the dialog sits on ONE line in a frame; a long one is cut
// in the middle with … (the caller copies the full text).
func TestCodeBoxOneLine(t *testing.T) {
	cmd := "scp 'fu@192.168.1.20:/home/fu/termocode/packaging/aur/PKGBUILD.tmpl' ."
	m := New("Download PKGBUILD.tmpl", "Run this on your computer:", []Button{{ID: "copy", Title: "Copy Command"}}).
		WithCode(cmd, "Tip: trzsz works in any terminal.")
	m.SetSize(120, 40)
	box := ansi.Strip(m.Box())
	if !strings.Contains(box, cmd) {
		t.Fatalf("command not on one line:\n%s", box)
	}
	if !strings.Contains(box, "╭") || !strings.Contains(box, "╰") {
		t.Errorf("no frame:\n%s", box)
	}
	if !strings.Contains(box, "Tip: trzsz") {
		t.Errorf("no note:\n%s", box)
	}
	// Narrow window: still one line, cut in the middle.
	m.SetSize(60, 40)
	box = ansi.Strip(m.Box())
	var row string
	for _, l := range strings.Split(box, "\n") {
		if strings.Contains(l, "scp 'fu@") {
			row = l
		}
	}
	if row == "" || !strings.Contains(row, "…") || !strings.Contains(row, "' .") {
		t.Fatalf("no cut command row:\n%s", box)
	}
	// The button is still where a click finds it.
	r := m.ButtonRects()
	if len(r) != 1 || m.ButtonAt(r[0].X+1, r[0].Y) != "copy" {
		t.Errorf("button rects %+v", r)
	}
	lines := strings.Split(box, "\n")
	_, y, _, _ := m.Bounds()
	if got := lines[r[0].Y-y]; !strings.Contains(got, "Copy Command") {
		t.Errorf("button row %d is %q", r[0].Y-y, got)
	}
}

func TestTruncMiddle(t *testing.T) {
	if got := truncMiddle("abcdefghij", 20); got != "abcdefghij" {
		t.Errorf("short: %q", got)
	}
	if got := truncMiddle("abcdefghij", 7); got != "abc…hij" {
		t.Errorf("cut: %q", got)
	}
}
