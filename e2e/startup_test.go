//go:build e2e

package e2e

import (
	"testing"
	"time"
)

// TestStartupSilentTerminal: a terminal that answers no queries (no OSC 11,
// no DA1, no CSI 6n — some muxers, IDE terminals, dumb ptys) must not keep
// the screen blank. Bubble Tea v1 asks lipgloss for the background colour at
// package init, and termenv waits 5 s for the OSC 11 reply.
func TestStartupSilentTerminal(t *testing.T) {
	tm := Start(t, StartOpts{Silent: true})
	const limit = 1500 * time.Millisecond
	deadline := time.Now().Add(5 * limit)
	for {
		tm.mu.Lock()
		at := tm.altAt
		tm.mu.Unlock()
		if !at.IsZero() {
			took := at.Sub(tm.started)
			t.Logf("alt screen after %s", took)
			if took > limit {
				t.Fatalf("alt screen took %s on a silent terminal, want <= %s", took, limit)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no alt screen within %s on a silent terminal", 5*limit)
		}
		time.Sleep(10 * time.Millisecond)
	}
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Quit()
}
