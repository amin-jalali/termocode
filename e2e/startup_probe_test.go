//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Terminal R3: a terminal that answers the startup probe after its
// deadline sends the answers to Bubble Tea. They must not type into the
// editor (the kitty graphics answer is an APC string: upstream read it as
// Alt+_ and the text "Gi=31;OK").
func TestProbeLateAnswersNotTyped(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send("\x1b_Gi=31;OK\x1b\\" + "\x1b[6;20;10t" + "\x1b[?0u" + "\x1bP>|kitty(0.36)\x1b\\" + "\x1b[?62;22c")
	tm.Type("Q")
	tm.WaitText("Qfirst note", uiTimeout)
	tm.settle()
	for _, junk := range []string{"Gi=31", ";OK", "kitty(0.36)", "62;22c"} {
		if tm.Has(junk) {
			t.Errorf("late probe answer %q typed into the editor:\n%s", junk, tm.Screen())
		}
	}
	tm.Send(KeyCtrlS) // no "unsaved changes" question on quit
	tm.settle()
	tm.Quit()
}

// Terminal R3: a silent terminal pays the probe timeout once. The next
// start (same HOME, same terminal) remembers it and sends no query.
func TestProbeSilentTerminalRemembered(t *testing.T) {
	first := Start(t, StartOpts{Silent: true})
	first.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	first.settle()
	if pre := beforeAlt(first.rawOutput()); !strings.Contains(pre, "\x1b[c") {
		t.Fatalf("first run sent no probe: %q", pre)
	}
	first.Quit()

	second := Start(t, StartOpts{Silent: true, Home: first.home, Workspace: first.ws})
	second.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	second.settle()
	if pre := beforeAlt(second.rawOutput()); strings.Contains(pre, "\x1b[c") || strings.Contains(pre, "\x1b[16t") {
		t.Errorf("second run on a known-silent terminal probed again: %q", pre)
	}
	second.Quit()
}

// beforeAlt is the output before the alt screen was entered.
func beforeAlt(out string) string {
	if i := strings.Index(out, "\x1b[?1049h"); i >= 0 {
		return out[:i]
	}
	return out
}
