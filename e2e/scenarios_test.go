//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	bootTimeout = 30 * time.Second
	uiTimeout   = 8 * time.Second
)

// mainFrame: the tab bar (with its "more" button ⋯) is row 0 and the status
// bar (encoding, "Ln N, Col M") is the last row.
var mainFrame = Frame{
	// ⋯ with Unicode icons, nf-fa-ellipsis_h with Nerd icons (kitty).
	Top: func(l string) bool { return strings.Contains(l, "⋯") || strings.Contains(l, "\uf141") },
	Bottom: func(l string) bool {
		// Narrow widths cut the right side ("UTF…"), so match any status
		// item; with the terminal focused the right side is the shell and
		// its folder, so the project pill (◆, or the Nerd folder) counts.
		return strings.Contains(l, "UTF") || strings.Contains(l, "Ln ") || strings.Contains(l, "Spaces:") ||
			strings.Contains(l, "◆ ") || strings.Contains(l, "\uf07c ")
	},
}

// boot starts termocode and waits for the dashboard.
func boot(t *testing.T, cols, rows int) *Term {
	t.Helper()
	tm := Start(t, StartOpts{Cols: cols, Rows: rows})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.CheckScreen("boot", mainFrame)
	return tm
}

// settle waits until the screen stops changing for a short while.
func (tm *Term) settle() { tm.WaitStable(300*time.Millisecond, uiTimeout) }

// step settles and runs the per-step screen checks.
func (tm *Term) step(name string) {
	tm.t.Helper()
	tm.settle()
	tm.CheckScreen(name, mainFrame)
}

// Palette runs a command palette entry by typing its title.
func (tm *Term) Palette(title string) {
	tm.t.Helper()
	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.Type(title)
	tm.WaitText(title, uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("Command Palette", uiTimeout)
}

// OpenFile opens a workspace file through Quick Open (Ctrl+P).
func (tm *Term) OpenFile(name string) {
	tm.t.Helper()
	tm.Send(KeyCtrlP)
	tm.WaitText("Go to file", uiTimeout)
	tm.Type(name)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("Go to file", uiTimeout)
	tm.WaitFor("tab "+name, uiTimeout, func(ls []string) bool { return strings.Contains(ls[0], name) })
}

// OpenTerminal opens the terminal panel (Ctrl+T) and waits for the shell
// prompt.
func (tm *Term) OpenTerminal() {
	tm.t.Helper()
	tm.Send(KeyCtrlT)
	tm.WaitFor("shell prompt", uiTimeout, func(ls []string) bool {
		for _, l := range ls {
			if strings.HasSuffix(l, "$") {
				return true
			}
		}
		return false
	})
	tm.settle()
}

func TestDashboard(t *testing.T) {
	tm := boot(t, 120, 36)
	for _, want := range []string{"Open file", "Command palette", "R E C E N T", "W O R K S P A C E S"} {
		if !tm.Has(want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}
	tm.Quit()
}

func TestOpenFileAndType(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenFile("main.go")
	tm.WaitText("func main()", uiTimeout)
	tm.step("opened main.go")

	// Wide glyphs must not shift the rest of the line (grid regression).
	tm.WaitText(`println("hi → 界")`, uiTimeout)

	// Type into a plain-text file: no LSP completion popups, so the result
	// does not depend on which language servers are installed.
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.step("opened notes.txt")
	// Esc first: after Quick Open the editor may be in Normal or Insert
	// mode; from Normal, "o" opens a new line in Insert mode.
	tm.Send(KeyEsc)
	tm.settle()
	tm.Type("ohello ⎇ ⌥ → — 界 world")
	tm.WaitText("hello ⎇ ⌥ → — 界 world", uiTimeout)
	tm.step("typed text")
	tm.Send(KeyEsc)
	tm.step("normal mode")

	tm.Send(KeyCtrlS)
	tm.WaitText("Saved", uiTimeout)
	tm.step("saved")
	tm.Quit()
	b, err := os.ReadFile(filepath.Join(tm.ws, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "hello ⎇ ⌥ → — 界 world\n") {
		t.Errorf("notes.txt on disk = %q", b)
	}
}

func TestSourceControlHover(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.Palette("Git: Focus Source Control")
	tm.WaitText("GRAPH", uiTimeout)
	tm.step("source control")

	// Commit rows of the graph: below the GRAPH header, in the sidebar.
	lines := tm.Lines()
	start := -1
	for y, l := range lines {
		if strings.Contains(l, "GRAPH") {
			start = y + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("no GRAPH header\n%s", tm.Screen())
	}
	var rows []int
	for y := start; y < len(lines)-1 && len(rows) < 20; y++ {
		if strings.Contains(lines[y], " now") {
			rows = append(rows, y)
		}
	}
	if len(rows) < 15 {
		t.Fatalf("want >= 15 commit rows, got %d\n%s", len(rows), tm.Screen())
	}
	cards := 0
	for _, y := range rows {
		tm.MouseMove(16, y)
		tm.settle()
		tm.CheckScreen(fmt.Sprintf("hover row %d", y), mainFrame)
		if tm.Has("E2E Tester") {
			cards++
		}
	}
	if cards == 0 {
		t.Errorf("no hover card appeared over %d commit rows", len(rows))
	}
	// Move away: the card goes, the frame stays.
	tm.MouseMove(80, 10)
	tm.WaitGone("E2E Tester", uiTimeout)
	tm.step("hover gone")

	// A modal over the graph (wide glyphs under the dimmed backdrop).
	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.step("palette over graph")
	tm.Send(KeyEsc)
	tm.WaitGone("Command Palette", uiTimeout)
	tm.step("palette closed")
	tm.Quit()
}

func TestTerminalPanel(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenTerminal()
	tm.step("terminal open")

	tm.Type("echo e2e-$((40+2)) ⎇ 界\r")
	tm.WaitText("e2e-42 ⎇ 界", uiTimeout)
	tm.step("terminal echo")

	tm.Send(KeyCtrlT)
	tm.WaitGone("e2e-42", uiTimeout)
	tm.step("terminal closed")

	// Closing the panel ends the shell; reopening starts a fresh one.
	tm.OpenTerminal()
	tm.step("terminal reopened")
	tm.Type("echo again-$((1+1))\r")
	tm.WaitText("again-2", uiTimeout)
	tm.step("terminal echo again")
	tm.Send(KeyCtrlT)
	tm.WaitGone("again-2", uiTimeout)
	tm.step("terminal closed again")
	tm.Quit()
}

func TestPalette(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.step("palette open")
	tm.Type("toggle explorer")
	tm.WaitText("View: Toggle Explorer", uiTimeout)
	tm.step("palette filtered")
	for i := 0; i < 5; i++ {
		tm.Send(KeyDown)
	}
	tm.step("palette moved")
	tm.Send(KeyEsc)
	tm.WaitGone("Command Palette", uiTimeout)
	tm.step("palette closed")

	tm.Palette("View: Toggle Explorer")
	tm.WaitGone("EXPLORER", uiTimeout)
	tm.step("explorer hidden")
	tm.Palette("View: Toggle Explorer")
	tm.WaitText("EXPLORER", uiTimeout)
	tm.step("explorer shown")
	tm.Quit()
}

func TestResize(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenFile("README.md")
	tm.step("readme")
	tm.OpenTerminal()
	tm.step("terminal")

	sizes := [][2]int{{80, 24}, {160, 50}, {60, 20}, {40, 12}, {100, 30}, {200, 60}, {120, 36}}
	for _, s := range sizes {
		tm.Resize(s[0], s[1])
		// Very small sizes may drop parts of the chrome; the scroll checks
		// (scrollback, UTF-8, U+FFFD) still hold. Full frame from 60 cols.
		f := Frame{
			Top:    func(l string) bool { return strings.TrimSpace(l) != "" },
			Bottom: func(l string) bool { return strings.TrimSpace(l) != "" },
		}
		if s[0] >= 60 {
			f = mainFrame
		}
		tm.WaitFor(fmt.Sprintf("redraw at %dx%d", s[0], s[1]), uiTimeout, func(ls []string) bool {
			return len(ls) == s[1] && f.Top(ls[0]) && f.Bottom(ls[len(ls)-1])
		})
		tm.settle()
		tm.CheckScreen(fmt.Sprintf("resize %dx%d", s[0], s[1]), f)
	}
	// The shell prompt sits inside the panel, below its tab bar (it used
	// to end up above it after a small-then-large resize).
	tm.WaitFor("prompt below the panel tab bar", uiTimeout, func(ls []string) bool {
		tab, prompt := -1, -1
		for y, l := range ls {
			if strings.Contains(l, "✕  +") {
				tab = y
			}
			if strings.HasSuffix(l, "$") {
				prompt = y
			}
		}
		return tab >= 0 && prompt > tab
	})
	// Ctrl+Q goes to the shell while the terminal has focus; close the
	// panel first.
	tm.Send(KeyCtrlT)
	tm.WaitGone("✕  +", uiTimeout)
	tm.step("terminal closed")
	tm.Quit()
}

func TestAIChatPanel(t *testing.T) {
	tm := boot(t, 140, 40)
	tm.OpenFile("main.go")
	tm.step("main.go")

	// Palette entry without a provider: only a toast.
	tm.Palette("AI: Open Chat")
	tm.WaitText("AI is not configured", uiTimeout)
	tm.step("ai toast")

	// Alt+A opens the AI / actions panel even without a provider.
	tm.Send("\x1ba")
	tm.settle()
	tm.CheckScreen("ai panel open", mainFrame)
	tm.Send("\x1ba")
	tm.step("ai panel closed")
	tm.Quit()
}
