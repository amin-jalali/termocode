//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// E2E-UI: named layouts, the key hint popup, terminal splits and the
// explorer preview + bulk rename/undo on the real binary.

const (
	KeyAltY     = "\x1by"
	KeyAltSlash = "\x1b/"
	KeyAltLeft  = "\x1b[1;3D"
	KeyCtrlB    = "\x02"
	KeyCtrlA    = "\x01"
	KeyShiftUp  = "\x1b[1;2A"
)

// hasShellPrompt: a screen line ends with the sh prompt.
func hasShellPrompt(ls []string) bool {
	for _, l := range ls {
		if strings.HasSuffix(l, "$") {
			return true
		}
	}
	return false
}

// textCol is the screen column where text starts on the lowest line that
// has it, or -1.
func (tm *Term) textCol(text string) int {
	ls := tm.Lines()
	for y := len(ls) - 1; y >= 0; y-- {
		if i := strings.Index(ls[y], text); i >= 0 {
			return ansi.StringWidth(ls[y][:i])
		}
	}
	return -1
}

// bootIn starts termocode in ws with home and waits for the main frame.
func bootIn(t *testing.T, home, ws string) *Term {
	t.Helper()
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, Home: home, Workspace: ws})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

// Layout: Save As… writes the open file and the terminal panel to a
// project layout; Alt+Y opens it again, also after a restart.
func TestUILayoutSaveOpen(t *testing.T) {
	home, ws := t.TempDir(), NewRepo(t)
	tm := bootIn(t, home, ws)
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.OpenTerminal()

	tm.Palette("Layout: Save As…")
	tm.WaitText("Name for this layout:", uiTimeout)
	tm.Type("e2e-ui")
	tm.Send(KeyEnter)
	tm.WaitText("Who gets this layout?", uiTimeout)
	tm.settle()
	tm.ClickText("This project")
	tm.WaitText("Layout e2e-ui saved", uiTimeout)
	file := filepath.Join(ws, ".termocode", "layouts", "e2e-ui.jsonc")
	if body := fileText(file); !strings.Contains(body, "notes.txt") {
		t.Fatalf("saved layout %s does not name notes.txt:\n%s", file, body)
	}

	// Change the screen: no terminal, no explorer, another file.
	tm.Palette("Terminal: Toggle Integrated Panel")
	tm.WaitFor("terminal hidden", uiTimeout, func(ls []string) bool { return !hasShellPrompt(ls) })
	tm.Send(KeyCtrlB)
	tm.WaitGone("EXPLORER", uiTimeout)
	tm.OpenFile("main.go")
	tm.settle()

	tm.Send(KeyAltY)
	tm.WaitText("Layout: Open", uiTimeout)
	tm.Type("e2e-ui")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitGone("Layout: Open", uiTimeout)
	tm.WaitText("EXPLORER", uiTimeout)
	tm.WaitFor("terminal back", uiTimeout, hasShellPrompt)
	tm.WaitFor("notes.txt tab", uiTimeout, func(ls []string) bool { return strings.Contains(ls[0], "notes.txt") })
	tm.step("layout opened")
	tm.Quit()

	// The project layout is still there after a restart.
	tm = bootIn(t, home, ws)
	tm.Send(KeyAltY)
	tm.WaitText("Layout: Open", uiTimeout)
	tm.WaitText("e2e-ui", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Layout: Open", uiTimeout)
	tm.Quit()
}

// keyHintInit maps a leader (Space) group in the user's init.lua; timeout
// off, so the editor waits for the next key and the popup stays.
const keyHintInit = `vim.g.mapleader = ' '
vim.o.timeout = false
vim.keymap.set('n', '<leader>a', function() vim.api.nvim_buf_set_lines(0, 0, 0, false, { 'E2E-HINT-RAN' }) end, { desc = 'E2E add line' })
vim.keymap.set('n', '<leader>gs', '<Nop>', { desc = 'E2E git one' })
vim.keymap.set('n', '<leader>gl', '<Nop>', { desc = 'E2E git two' })
`

// After the leader key the popup lists the next keys; g goes one level
// down, Esc closes it, a mapped key runs; key_hints off: no popup.
func TestUIKeyHintPopup(t *testing.T) {
	tm := Start(t, StartOpts{Cols: 120, Rows: 36, NvimInit: keyHintInit})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.OpenFile("notes.txt")
	tm.WaitText("first note", uiTimeout)
	tm.Send(KeyEsc) // Normal mode
	tm.settle()

	tm.Send(" ")
	tm.WaitText("E2E add line", uiTimeout)
	tm.step("hints after leader")
	tm.Send("g")
	tm.WaitText("Space g", uiTimeout)
	tm.WaitText("E2E git one", uiTimeout)
	tm.WaitText("E2E git two", uiTimeout)
	tm.WaitGone("E2E add line", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("E2E git one", uiTimeout)

	tm.Send(" ")
	tm.WaitText("E2E add line", uiTimeout)
	tm.Send("a")
	tm.WaitText("E2E-HINT-RAN", uiTimeout)
	tm.WaitGone("E2E add line", uiTimeout)

	tm.Palette("Help: Toggle Key Hints")
	tm.WaitText("Key hints off", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Send(" ")
	// Longer than the popup delay (400 ms): the popup would break the quiet.
	tm.WaitStable(time.Second, uiTimeout)
	if tm.Has("E2E add line") {
		t.Fatalf("key_hints off, popup shown:\n%s", tm.Screen())
	}
	tm.Send(KeyEsc)
	// Esc and Ctrl+Q in one read are one key, Alt+Ctrl+Q (legacy
	// encoding): let the Esc arrive alone first.
	tm.settle()
	tm.quitDiscard()
}

// Alt+/ splits the terminal to the right with a new shell; Alt+Left goes
// back to the left pane; Terminal: Close Pane closes the focused pane.
func TestUITerminalSplits(t *testing.T) {
	tm := boot(t, 120, 36)
	tm.OpenTerminal()
	// $((1+1)): the output (LEFT_2) differs from the typed command.
	tm.Type("echo LEFT_$((1+1))")
	tm.Send(KeyEnter)
	tm.WaitText("LEFT_2", uiTimeout)

	tm.Send(KeyAltSlash)
	tm.settle()
	tm.Type("echo RIGHT_$((1+1))")
	tm.Send(KeyEnter)
	tm.WaitText("RIGHT_2", uiTimeout)
	if !tm.Has("LEFT_2") {
		t.Fatalf("left pane gone after the split:\n%s", tm.Screen())
	}
	left, right := tm.textCol("LEFT_2"), tm.textCol("RIGHT_2")
	if right <= left+10 {
		t.Fatalf("RIGHT_2 at col %d, LEFT_2 at col %d: not side by side\n%s", right, left, tm.Screen())
	}
	tm.step("split right")

	tm.Send(KeyAltLeft)
	tm.settle()
	tm.Type("echo BACK_$((1+1))")
	tm.Send(KeyEnter)
	tm.WaitText("BACK_2", uiTimeout)
	if back := tm.textCol("BACK_2"); back >= right {
		t.Fatalf("BACK_2 at col %d, not in the left pane (right pane at %d)\n%s", back, right, tm.Screen())
	}

	tm.Palette("Terminal: Close Pane")
	tm.WaitGone("BACK_2", uiTimeout)
	tm.WaitGone("LEFT_2", uiTimeout)
	if !tm.Has("RIGHT_2") {
		t.Fatalf("right pane gone with the left one:\n%s", tm.Screen())
	}
	tm.step("left pane closed")
	tm.Quit()
}

// explorerWorkspace has only two files, so they are the last two rows of
// the tree (folders come first).
func explorerWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		ws = real
	}
	gitIn(t, ws, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(ws, "a1.txt"), "PREVIEW-ONE\n")
	writeFile(t, filepath.Join(ws, "a2.txt"), "PREVIEW-TWO\n")
	return ws
}

// p shows the file under the cursor in the explorer; Shift+Up selects two
// files, r renames both with a preview card first, Apply renames them and
// Explorer: Undo Rename puts the old names back.
func TestUIExplorerPreviewBulkRename(t *testing.T) {
	ws := explorerWorkspace(t)
	tm := bootIn(t, t.TempDir(), ws)
	tm.Palette("View: Focus Explorer")
	tm.WaitText("a2.txt", uiTimeout)
	tm.Send("G") // last row: a2.txt
	tm.settle()

	tm.Send("p")
	tm.WaitText("PREVIEW-TWO", uiTimeout)
	tm.step("explorer preview")
	tm.Send("p")
	tm.WaitGone("PREVIEW-TWO", uiTimeout)

	tm.Send(KeyShiftUp) // a1.txt and a2.txt
	tm.settle()
	tm.Send("r")
	tm.WaitText("Rename selected files", uiTimeout)
	tm.settle()
	tm.Type("old-") // the caret starts on the first name
	tm.Send(KeyDown)
	tm.Send(KeyCtrlA)
	tm.Type("old-")
	tm.Send(KeyCtrlS)
	tm.WaitText("Rename preview", uiTimeout)
	tm.WaitText("a1.txt → old-a1.txt", uiTimeout)
	tm.WaitText("a2.txt → old-a2.txt", uiTimeout)
	if !exists(filepath.Join(ws, "a1.txt")) {
		t.Fatalf("a1.txt renamed before Apply")
	}
	tm.settle()
	tm.ClickText("Apply")
	tm.WaitText("Renamed 2 files", uiTimeout)
	for _, n := range []string{"old-a1.txt", "old-a2.txt"} {
		if !exists(filepath.Join(ws, n)) {
			t.Fatalf("%s missing after the rename", n)
		}
	}
	if exists(filepath.Join(ws, "a1.txt")) || exists(filepath.Join(ws, "a2.txt")) {
		t.Fatalf("old names still on disk after the rename")
	}
	tm.WaitText("old-a1.txt", uiTimeout)

	tm.Palette("Explorer: Undo Rename")
	tm.WaitText("Rename undone", uiTimeout)
	for _, n := range []string{"a1.txt", "a2.txt"} {
		b, err := os.ReadFile(filepath.Join(ws, n))
		if err != nil {
			t.Fatalf("%s not back after undo: %v", n, err)
		}
		if n == "a1.txt" && string(b) != "PREVIEW-ONE\n" {
			t.Fatalf("a1.txt body %q after undo", b)
		}
	}
	if exists(filepath.Join(ws, "old-a1.txt")) {
		t.Fatalf("old-a1.txt still on disk after undo")
	}
	tm.WaitGone("old-a1.txt", uiTimeout)
	tm.Quit()
}
