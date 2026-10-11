package specui

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// termDriver starts the app with nvim and a plain /bin/sh (no user rc
// files, a "$ " prompt) and opens main.go.
func termDriver(t *testing.T) *testkit.Driver {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PS1", "$ ")
	return editorWith(t, nil, "main.go", "fmt.Println")
}

// openTerminal opens the panel and waits for the shell prompt.
func openTerminal(t *testing.T, d *testkit.Driver) {
	t.Helper()
	d.Key("ctrl+t")
	mustSee(t, d, "Terminal › sh")
	d.Settle(300 * time.Millisecond)
}

// run types a shell line and waits for its output marker.
func run(t *testing.T, d *testkit.Driver, line, marker string) {
	t.Helper()
	d.Type(line)
	d.Key("enter")
	mustSee(t, d, marker)
}

// terminalTabs counts the tabs on the terminal tab bar ("name ×" each;
// the panel's own close button is "−×").
func terminalTabs(d *testkit.Driver) int {
	return strings.Count(lineWith(d, "↑−×"), " ×")
}

// Spec: Given the editor,
// when the user presses Ctrl+T and types a command,
// then the panel opens with a shell and the command's output shows;
// Ctrl+T hides the panel (the editor stays) and the next Ctrl+T shows the
// same shell again, with its output and its running jobs.
func TestTerminalToggleAndRunCommand(t *testing.T) {
	d := termDriver(t)
	openTerminal(t, d)
	run(t, d, "echo MARK$((40+2))", "MARK42")
	d.Type("(sleep 1; echo BG$((7))) &")
	d.Key("enter")
	d.Key("ctrl+t")
	mustNotSee(t, d, "MARK42")
	mustSee(t, d, "fmt.Println")
	d.Settle(1200 * time.Millisecond) // the background job ends while hidden
	d.Key("ctrl+t")
	mustSee(t, d, "MARK42") // the same shell comes back
	mustSee(t, d, "BG7")
	run(t, d, "echo STILL$((8))", "STILL8")
}

// Spec: Given an open terminal,
// when the user adds a tab, runs a command in each, and switches back,
// then each tab shows its own output; closing a tab leaves the other,
// and closing the last tab closes the panel.
func TestTerminalTabs(t *testing.T) {
	d := termDriver(t)
	openTerminal(t, d)
	run(t, d, "echo ONE$((1))", "ONE1")
	d.Palette("terminal-new-tab")
	d.Settle(500 * time.Millisecond)
	if n := terminalTabs(d); n != 2 {
		t.Fatalf("after New Tab: %d tabs\n%s", n, lineWith(d, "↑−×"))
	}
	mustNotSee(t, d, "ONE1")
	run(t, d, "echo TWO$((2))", "TWO2")
	d.Palette("terminal-prev-tab")
	mustSee(t, d, "ONE1")
	mustNotSee(t, d, "TWO2")
	d.Palette("terminal-close-tab")
	mustSee(t, d, "TWO2")
	if n := terminalTabs(d); n != 1 {
		t.Errorf("after Close Tab: %d tabs", n)
	}
	d.Palette("terminal-close-tab")
	mustNotSee(t, d, "↑−×")
}

// Spec: Given a long-running command in the terminal,
// when the user presses Ctrl+C,
// then the shell gets SIGINT (the app does not copy) and the next
// command runs.
func TestTerminalCtrlCInterrupts(t *testing.T) {
	d := termDriver(t)
	openTerminal(t, d)
	d.Type("sleep 30")
	d.Key("enter")
	d.Settle(300 * time.Millisecond)
	d.Key("ctrl+c")
	run(t, d, "echo AFTER$((3))", "AFTER3")
}

// Spec: Given a terminal with 300 lines of output,
// when the user turns the mouse wheel up over the panel,
// then older lines show (scrollback) and the last line is gone; typing
// a command returns to the prompt and runs it.
func TestTerminalWheelScrollback(t *testing.T) {
	d := termDriver(t)
	openTerminal(t, d)
	run(t, d, "seq 1 300 | sed s/^/L/", "L300")
	x, y, _ := locate(d, "L299")
	for range 4 {
		d.Wheel(x, y, true)
		d.Settle(100 * time.Millisecond)
	}
	if s := screen(d); strings.Contains(s, "L300") || !strings.Contains(s, "L28") {
		t.Fatalf("wheel up did not scroll back:\n%s", s)
	}
	run(t, d, "echo BACK$((4))", "BACK4")
}

// Spec: Given an open terminal,
// when the window gets smaller and then bigger,
// then the shell still runs commands and the frame stays valid.
func TestTerminalSurvivesResize(t *testing.T) {
	d := termDriver(t)
	openTerminal(t, d)
	d.Resize(80, 24)
	run(t, d, "echo SMALL$((5))", "SMALL5")
	d.Resize(140, 44)
	run(t, d, "echo BIG$((6))", "BIG6")
}
