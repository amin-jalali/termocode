//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Terminal R1: on a terminal that answers the kitty keyboard query,
// termocode turns the protocol on inside the alt screen (and only there),
// keys sent as CSI u work, and the protocol is turned off before the alt
// screen is left, so the shell gets its legacy keys back.
func TestKittyKeyboardProtocol(t *testing.T) {
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, KittyKeys: true})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	out := tm.rawOutput()
	enter, push := strings.Index(out, "\x1b[?1049h"), strings.Index(out, "\x1b[>1u")
	if enter < 0 || push < enter {
		t.Fatalf("protocol not pushed after the alt screen: enter=%d push=%d", enter, push)
	}

	// Ctrl+Shift+M: only the kitty protocol can send it.
	tm.Send("\x1b[109;6u")
	tm.WaitText("PROBLEMS", uiTimeout)
	// Alt+X (legacy-equivalent combo, CSI u form) opens the palette, Esc
	// (CSI 27 u) closes it.
	tm.Send("\x1b[120;3u")
	tm.WaitText("Command Palette", uiTimeout)
	tm.Send("\x1b[27u")
	tm.WaitGone("Command Palette", uiTimeout)

	tm.Quit()
	out = tm.rawOutput()
	pop, leave := strings.LastIndex(out, "\x1b[<u"), strings.LastIndex(out, "\x1b[?1049l")
	if pop < 0 || leave < 0 || pop > leave {
		t.Fatalf("protocol not popped before leaving the alt screen: pop=%d leave=%d", pop, leave)
	}
}

// A terminal that does not answer never gets the protocol.
func TestKittyKeyboardOffWithoutAnswer(t *testing.T) {
	tm := boot(t, 100, 30)
	if strings.Contains(tm.rawOutput(), "\x1b[>1u") {
		t.Fatal("kitty keyboard protocol pushed to a terminal that did not answer")
	}
	tm.Quit()
}
