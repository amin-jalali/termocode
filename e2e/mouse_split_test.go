//go:build e2e

package e2e

import (
	"testing"
	"time"
)

// Terminal R6: with all-motion tracking a mouse report may arrive in two
// reads. The halves must be one mouse event, never text typed into the
// editor ("[<35;70;" then "19M").
func TestSplitMouseReportNotTyped(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	for i := 0; i < 5; i++ {
		tm.Send("\x1b[<35;70;")
		time.Sleep(5 * time.Millisecond)
		tm.Send("19M")
		time.Sleep(5 * time.Millisecond)
	}
	tm.Type("Q")
	tm.WaitText("Qfirst note", uiTimeout)
	tm.settle()
	for _, junk := range []string{"<35", "19M", "70;"} {
		if tm.Has(junk) {
			t.Errorf("mouse fragment %q typed:\n%s", junk, tm.Screen())
		}
	}
	tm.Send(KeyCtrlS)
	tm.settle()
	tm.Quit()
}

// Terminal R6: plain motion (1003) only while hover is useful: the window
// in the background gets button events only (1002), and 1003 comes back
// with the focus.
func TestAllMotionOffInBackground(t *testing.T) {
	tm := boot(t, 100, 30)
	mode := func(m string) bool {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		return tm.modes[m]
	}
	waitMode := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(uiTimeout)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("%s: modes %v", what, tm.modes)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitMode("1003 at start", func() bool { return mode("?1003") })
	tm.Send("\x1b[O") // focus out
	waitMode("1002 in the background", func() bool { return !mode("?1003") && mode("?1002") })
	tm.Send("\x1b[I") // focus in
	waitMode("1003 with the focus", func() bool { return mode("?1003") })
	tm.Quit()
}

// mouse_hover = off: button events only, from the start.
func TestMouseHoverOff(t *testing.T) {
	home := t.TempDir()
	writeFile(t, home+"/.config/termocode/config.json", `{"mouse_hover": "off"}`)
	tm := Start(t, StartOpts{Home: home})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.mu.Lock()
	all, cell := tm.modes["?1003"], tm.modes["?1002"]
	tm.mu.Unlock()
	if all || !cell {
		t.Errorf("mouse_hover off: 1003=%v 1002=%v, want 1002 only", all, cell)
	}
	tm.Quit()
}
