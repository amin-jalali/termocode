//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Terminal R14: on the Linux console (TERM=linux) termocode asks the
// terminal nothing, sends no background colour and no bidi request, and
// clears the screen on the way out (the console has no alternate screen,
// so the IDE frame would stay on the TTY).
func TestLinuxConsole(t *testing.T) {
	tm := Start(t, StartOpts{Env: []string{"TERM=linux"}})
	// ASCII icons: the top row has no "⋯".
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return strings.Contains(ls[0], "EXPLORER") && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Quit()
	out := tm.rawOutput()
	for _, bad := range []string{"\x1b[c", "\x1b[16t", "\x1b[?u", "\x1b]11;", "\x1b[8l"} {
		if strings.Contains(out, bad) {
			t.Errorf("sent %q to the Linux console", bad)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\r\n"), "\x1b[2J\x1b[H") {
		end := out[max(0, len(out)-80):]
		t.Errorf("screen not cleared at exit; output ends %q", end)
	}
}
