//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// SCN-5: what the user set up comes back after a full restart (a clean
// Ctrl+Q, no session server): the terminal tabs with their folders, and a
// saved layout that can be opened again and then stays.

// startAt starts termocode on ws with home and waits for the editor frame.
func startAt(t *testing.T, home, ws string, env []string) *Term {
	t.Helper()
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Home: home, Workspace: ws, Env: env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// clickPrompt clicks the lowest shell prompt ("$") on screen, which gives
// the keyboard to that terminal.
func (tm *Term) clickPrompt() {
	tm.t.Helper()
	tm.WaitFor("shell prompt", uiTimeout, func(ls []string) bool {
		for _, l := range ls {
			if strings.HasSuffix(l, "$") {
				return true
			}
		}
		return false
	})
	ls := tm.Lines()
	for y := len(ls) - 1; y >= 0; y-- {
		if strings.HasSuffix(ls[y], "$") {
			tm.Click(ansi.StringWidth(ls[y])-1, y)
			tm.settle()
			return
		}
	}
}

// Spec (SCN-5): two terminal tabs, the second one cd'd into a folder.
// After Ctrl+Q and a new start the panel is open again with both tabs, the
// second one on show and its shell in that folder, the first one in the
// project folder.
func TestSCNTerminalsAfterRestart(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	home, ws := t.TempDir(), NewRepo(t)
	if err := os.Mkdir(filepath.Join(ws, "scn5dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"SHELL=" + bash, "PS1=$ "}

	tm := startAt(t, home, ws, env)
	tm.OpenTerminal()
	tm.step("first tab")
	tm.Palette("Terminal: New Tab")
	tm.clickPrompt()
	tm.Type("cd scn5dir\r")
	// The tab is named after the folder (the shell's OSC 7): the cd is seen.
	tm.WaitText("$ scn5dir ", uiTimeout)
	tm.step("second tab in scn5dir")
	tm.Quit()

	tm2 := startAt(t, home, ws, env)
	tm2.WaitText("$ scn5dir ", bootTimeout)
	tm2.step("terminals restored")
	tm2.clickPrompt()
	tm2.Type(`echo "at-$(basename "$PWD")"` + "\r")
	tm2.WaitText("at-scn5dir", uiTimeout)
	tm2.step("second tab shell in scn5dir")

	tm2.Palette("Terminal: Previous Tab")
	tm2.clickPrompt()
	tm2.Type(`echo "at-$(basename "$PWD")"` + "\r")
	tm2.WaitText("at-"+filepath.Base(ws), uiTimeout)
	tm2.step("first tab shell in the project folder")
	tm2.Quit()
}

// Spec (SCN-5): Layout: Save As… with the explorer hidden and main.go
// open, saved for This project, writes .termocode/layouts/scn5.jsonc.
// The explorer is shown again and termocode restarts: it starts with the
// explorer (the session), Layout: Open… scn5 hides it and opens main.go,
// and the next start keeps that look.
func TestSCNSavedLayoutAfterRestart(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)

	tm := startAt(t, home, ws, nil)
	tm.OpenFile("main.go")
	tm.Palette("View: Toggle Explorer")
	tm.WaitGone("EXPLORER", uiTimeout)
	tm.step("explorer hidden")

	tm.Palette("Layout: Save As")
	tm.WaitText("Name for this layout:", uiTimeout)
	tm.Type("scn5")
	tm.Send(KeyEnter)
	tm.WaitText("Who gets this layout?", uiTimeout)
	tm.settle()
	tm.ClickText("This project")
	tm.WaitText("Layout scn5 saved", uiTimeout)
	tm.step("layout saved")
	if _, err := os.Stat(filepath.Join(ws, ".termocode", "layouts", "scn5.jsonc")); err != nil {
		t.Fatalf("layout file: %v", err)
	}

	tm.Palette("View: Toggle Explorer")
	tm.WaitText("EXPLORER", uiTimeout)
	tm.step("explorer shown")
	tm.Quit()

	tm2 := startAt(t, home, ws, nil)
	tm2.WaitText("EXPLORER", uiTimeout)
	tm2.step("restart keeps the explorer")
	tm2.Palette("Layout: Open")
	tm2.WaitText("Enter opens a layout", uiTimeout)
	tm2.Type("scn5")
	tm2.settle()
	tm2.Send(KeyEnter)
	tm2.WaitGone("EXPLORER", uiTimeout)
	tm2.WaitFor("main.go tab", uiTimeout, func(ls []string) bool { return strings.Contains(ls[0], "main.go") })
	tm2.step("layout scn5 opened")
	tm2.Quit()

	tm3 := startAt(t, home, ws, nil)
	if tm3.Has("EXPLORER") {
		t.Errorf("explorer back after a restart; the opened layout hid it:\n%s", tm3.Screen())
	}
	tm3.WaitFor("main.go tab", uiTimeout, func(ls []string) bool { return strings.Contains(ls[0], "main.go") })
	tm3.step("restart keeps layout scn5")
	tm3.Quit()
}
