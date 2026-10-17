//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// Terminal R10: on a terminal that answered DECRQM for mode 2026, every
// frame is drawn between ?2026h and ?2026l (balanced, never nested, the
// last one closed); on one that did not answer, mode 2026 is never sent.
func TestSynchronizedOutput(t *testing.T) {
	for _, c := range []struct {
		name string
		opts StartOpts
		sync bool
	}{
		{"answered", StartOpts{SyncOutput: true}, true},
		{"not answered", StartOpts{}, false},
		{"forced off", StartOpts{SyncOutput: true, Env: []string{"TERMOCODE_SYNC=off"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			tm := Start(t, c.opts)
			tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
				return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
			})
			tm.settle()
			tm.OpenFile("notes.txt")
			tm.WaitText("first note", uiTimeout)
			tm.Quit()
			out := tm.rawOutput()
			if !c.sync {
				if strings.Contains(out, "\x1b[?2026h") {
					t.Fatal("mode 2026 sent to a terminal that did not ask for it")
				}
				return
			}
			open, frames := false, 0
			for i := 0; i < len(out); {
				h := strings.Index(out[i:], "\x1b[?2026h")
				l := strings.Index(out[i:], "\x1b[?2026l")
				switch {
				case h < 0 && l < 0:
					i = len(out)
				case h >= 0 && (l < 0 || h < l):
					if open {
						t.Fatalf("nested ?2026h at %d", i+h)
					}
					open, frames, i = true, frames+1, i+h+1
				default:
					if !open {
						t.Fatalf("?2026l without ?2026h at %d", i+l)
					}
					open, i = false, i+l+1
				}
			}
			if open {
				t.Error("last frame left open (?2026h without ?2026l)")
			}
			if frames < 3 {
				t.Errorf("only %d synchronized frames", frames)
			}
		})
	}
}
