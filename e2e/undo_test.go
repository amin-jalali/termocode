//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Bug report 2026-10-15: "Ctrl+Z does not work". Ctrl+Z as the real byte
// (0x1A) from a legacy terminal, and as CSI u from a kitty-protocol one,
// undoes typing in the editor.
func TestCtrlZUndoesTyping(t *testing.T) {
	for _, c := range []struct {
		name  string
		kitty bool
		key   string
	}{{"legacy", false, "\x1a"}, {"kitty", true, "\x1b[122;5u"}} {
		t.Run(c.name, func(t *testing.T) {
			tm := Start(t, StartOpts{Cols: 120, Rows: 36, KittyKeys: c.kitty})
			tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
				return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
			})
			tm.settle()
			tm.OpenFile("notes.txt")
			tm.WaitText("first note", uiTimeout)
			tm.Type("XYZZY ")
			tm.WaitText("XYZZY first note", uiTimeout)
			tm.Send(c.key)
			tm.WaitGone("XYZZY", uiTimeout)
			tm.Quit()
		})
	}
}

// Ctrl+Z undoes in every mode: after Esc (normal mode, with another file
// in the jump list) and with text selected (Shift+arrows).
func TestCtrlZUndoesInEveryMode(t *testing.T) {
	tm := Start(t, StartOpts{Cols: 120, Rows: 36})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenFile("README.md")
	tm.WaitText("# demo", uiTimeout)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)

	// Normal mode.
	tm.Type("XYZZY ")
	tm.WaitText("XYZZY first note", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send("\x1a")
	tm.WaitGone("XYZZY", uiTimeout)
	if !strings.Contains(tm.Screen(), "first note") {
		t.Fatalf("Ctrl+Z in normal mode left notes.txt:\n%s", tm.Screen())
	}

	// With a selection: type, select two characters, Ctrl+Z.
	tm.Send("i")
	tm.Type("QWERT ")
	tm.WaitText("QWERT first note", uiTimeout)
	tm.Send("\x1b[1;2D\x1b[1;2D") // Shift+Left twice
	tm.settle()
	tm.Send("\x1a")
	tm.WaitGone("QWERT", uiTimeout)
	tm.Quit()
}
