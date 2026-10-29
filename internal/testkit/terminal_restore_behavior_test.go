package testkit

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Spec (G-RST): Given a bash terminal tab moved to notes/ with `cd`, split
// right, and running `sleep 300`,
// when the user quits and starts termocode again,
// then the tab comes back with its two panes, its shell in notes/, and the
// pane says what was running (only the program name).
func TestTerminalTabsComeBackAfterRestart(t *testing.T) {
	needBash(t)
	d := editorDriver(t, nil)
	open(d, "a.txt")
	openBashTerminal(d)
	termSend(d, 0, "cd notes\n")
	waitTerm(d, 0, "cwd did not follow cd", func(s app.TerminalTabState) bool {
		return s.Cwd == filepath.Join(d.Dir, "notes")
	})
	d.Palette("terminal-split-right")
	waitTerm(d, 0, "no second pane", func(s app.TerminalTabState) bool { return s.Panes == 2 })
	termSend(d, 0, "sleep 300 # secret-arg\n")
	waitTerm(d, 0, "sleep is not running", func(s app.TerminalTabState) bool { return s.Busy })
	d.Settle(300 * time.Millisecond) // a state message saves the busy pane

	d.Restart()
	st := waitTerm(d, 0, "terminal did not come back", func(s app.TerminalTabState) bool {
		return s.Panes == 2 && s.Cwd == filepath.Join(d.Dir, "notes")
	})
	if st.Busy || st.Hidden {
		t.Errorf("restored tab = %+v, want a fresh shell on show", st)
	}
	if !d.WaitForText("was running: sleep", 5*time.Second) {
		t.Errorf("no hint of the program that ran:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "secret-arg") {
		t.Errorf("the command line must not be kept by default:\n%s", screen(d))
	}
}
