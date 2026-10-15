//go:build e2e

package e2e

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
	"time"
)

var osc52Re = regexp.MustCompile("\x1b\\]52;c;([A-Za-z0-9+/=]+)\x07")

// copyRef runs "Copy: File:Line Reference" on main.go and returns the
// output it caused.
func copyRef(t *testing.T, tm *Term) string {
	t.Helper()
	tm.OpenFile("main.go")
	tm.WaitText("func main()", uiTimeout)
	tm.settle()
	off := tm.rawLen()
	tm.Palette("Copy: File:Line Reference")
	tm.WaitText("Copied", uiTimeout)
	time.Sleep(100 * time.Millisecond)
	return string(tm.rawSince(off))
}

// Spec (Group S1): a copy on the server reaches the user's own clipboard:
// over SSH (persistent session on) the OSC 52 sequence travels through
// the session to the terminal; inside tmux it is also sent as a tmux
// passthrough.
func TestSSHClipboardOSC52(t *testing.T) {
	t.Run("ssh session", func(t *testing.T) {
		r := newSessionRig(t)
		tm := r.start()
		out := copyRef(t, tm)
		m := osc52Re.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("no OSC 52 in the output: %q", out)
		}
		b, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil || !strings.Contains(string(b), "main.go:1:1") {
			t.Errorf("OSC 52 carries %q (%v), want the main.go:1:1 reference", b, err)
		}
		tm.Quit()
	})
	t.Run("ssh in tmux", func(t *testing.T) {
		tm := Start(t, StartOpts{Env: []string{
			"SSH_CONNECTION=10.0.0.1 50000 10.0.0.2 22",
			"TMUX=/tmp/tmux-1000/default,1,0", "TERM=screen-256color",
		}})
		tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
			return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
		})
		tm.settle()
		out := copyRef(t, tm)
		if !osc52Re.MatchString(out) {
			t.Errorf("no raw OSC 52 for tmux set-clipboard: %q", out)
		}
		if !strings.Contains(out, "\x1bPtmux;\x1b\x1b]52;c;") {
			t.Errorf("no tmux passthrough copy: %q", out)
		}
		tm.Quit()
	})
}
