package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/confirm"
)

// AH-1: "Discard changes?" throws away the file's edits for good. Cancel
// is the first, focused button; Enter or Cancel keeps the edits, only
// Discard reverts the file. Both ways in (x on the panel and the palette
// for the open file) open the same card.
func TestCovGitDiscardCardCancelFirst(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "a.txt", "edited\n")
	covRefresh(m)
	read := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
		return string(b)
	}

	open := map[string]func(){
		"panel x": func() {
			covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowFile })
			covKey(m, covRunes("x"))
		},
		"palette": func() {
			m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "")
			m.openGitDiscardForActiveFile()
		},
	}
	for name, o := range open {
		m.confirmOpen = false
		o()
		checkCardRule(t, name, *m, true)
		if got := m.confirm.SafeID(); got != "cancel" {
			t.Errorf("%s: card not built with confirm.Risky (SafeID %q)", name, got)
		}
		if b := m.confirm.Buttons(); b[0].ID != "cancel" || b[0].Style == confirm.StyleDestructive {
			t.Errorf("%s: buttons %+v", name, b)
		}
		m.confirmOpen = false
		m.handleConfirmSelect("cancel")
		if read() != "edited\n" {
			t.Fatalf("%s: Cancel discarded the edits", name)
		}
	}

	m.confirmOpen = false
	m.handleConfirmSelect("discard")
	if read() != "one\ntwo\nthree\n" {
		t.Errorf("Discard did not revert the file: %q", read())
	}
	if !toastHas(m, "Discarded changes") {
		t.Error("no Discarded toast")
	}

	// A path git does not know: the discard fails and says so.
	m.confirmTargetPath = "nope.txt"
	m.handleConfirmSelect("discard")
	if !toastHas(m, "Discard failed") {
		t.Error("no Discard failed toast")
	}

	// No file under the cursor, no open file: no card.
	m.confirmOpen = false
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowSection })
	covKey(m, covRunes("x"))
	m.editor.SetMeta("", false, "")
	m.openGitDiscardForActiveFile()
	if m.confirmOpen {
		t.Error("a card opened with no file")
	}
}
