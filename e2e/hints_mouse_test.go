//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// clickLast clicks the last place text appears on screen (footers are at
// the bottom of their card).
func (tm *Term) clickLast(text string) {
	tm.t.Helper()
	ls := tm.Lines()
	for y := len(ls) - 1; y >= 0; y-- {
		if i := strings.LastIndex(ls[y], text); i >= 0 {
			tm.Click(len([]rune(ls[y][:i]))+1, y)
			return
		}
	}
	tm.t.Fatalf("%q not on screen:\n%s", text, tm.Screen())
}

// Spec (2026-10-15, "mouse & keyboard should work in every menu"): the
// footer hints of the palette, the input prompt and the confirm dialog
// are buttons in the real terminal path.
func TestFooterHintClicks(t *testing.T) {
	tm := boot(t, 120, 36)

	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.settle()
	tm.clickLast("Esc")
	tm.WaitGone("Command Palette", uiTimeout)

	tm.Send("\x07") // Ctrl+G: go to line prompt
	tm.WaitText("Line number", uiTimeout)
	tm.settle()
	tm.clickLast("Esc")
	tm.WaitGone("Line number", uiTimeout)

	// Confirm dialog: discard a saved change to notes.txt.
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Type("X")
	tm.Send("\x13") // Ctrl+S
	tm.settle()
	tm.Palette("Git: Discard Changes in Current File")
	tm.WaitText("Discard", uiTimeout)
	tm.settle()
	tm.clickLast("Esc")
	tm.WaitGone("Esc  cancel", uiTimeout)
	if !strings.Contains(tm.Screen(), "Xfirst note") {
		t.Errorf("Esc on the confirm dialog discarded the change:\n%s", tm.Screen())
	}
	tm.Quit()
}
