package testkit

import (
	"strings"
	"testing"
	"time"
)

// Line staging from the editor: the cursor line (or the visual selection)
// goes to the index, and nothing else.

const linesBase = "l1\nl2\nl3\nl4\nl5\nl6\n"

// linesDriver starts the app on a git workspace whose f.txt has two edits
// (lines 2 and 4) on disk, with f.txt open in the editor.
func linesDriver(t *testing.T) *Driver {
	t.Helper()
	d := New0(t)
	open(d, "f.txt")
	writeWS(d, "f.txt", "l1\nX2\nl3\nX4\nl5\nl6\n")
	reloadFile(d, "X4")
	return d
}

// New0 is the editor driver with git and the f.txt workspace.
func New0(t *testing.T) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	return New(t, Options{W: 120, H: 34, Nvim: true, Git: true, Files: map[string]string{"f.txt": linesBase}})
}

// reloadFile makes the open buffer show what is on disk.
func reloadFile(d *Driver, want string) {
	d.T.Helper()
	lua(d, `vim.cmd('silent! checktime'); return ''`)
	if !d.WaitFor(func() bool { return strings.Contains(bufLines(d), want) }, 3*time.Second) {
		d.T.Fatalf("buffer %q, want %q", bufLines(d), want)
	}
}

// gotoLine puts the cursor on line n and lets the app see it.
func gotoLine(d *Driver, n string) {
	d.T.Helper()
	lua(d, `vim.api.nvim_win_set_cursor(0, {tonumber(...), 0}); return ''`, n)
	d.Settle(0)
}

func TestStageLineStagesOnlyTheCursorLine(t *testing.T) {
	d := linesDriver(t)
	gotoLine(d, "4")
	d.Palette("git-stage-lines")
	if !eventually(d, func() bool { return gitOut(d, "diff", "--cached", "--name-only") == "f.txt" }) {
		t.Fatalf("nothing staged:\n%s", screen(d))
	}
	if got := gitOut(d, "show", ":f.txt"); got != "l1\nl2\nl3\nX4\nl5\nl6" {
		t.Errorf("index: %q", got)
	}
	if got := diskFile(d, "f.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("the file on disk must not change: %q", got)
	}
}

func TestStageLineMovesToTheNextLine(t *testing.T) {
	d := linesDriver(t)
	gotoLine(d, "2")
	d.Palette("git-stage-lines")
	if !eventually(d, func() bool { return cursor(d) == "3:1" }) {
		t.Errorf("cursor %s, want 3:1", cursor(d))
	}
}

func TestUnstageLineTakesOnlyTheCursorLineOut(t *testing.T) {
	d := linesDriver(t)
	gitOut(d, "add", "f.txt") // both edits staged
	gotoLine(d, "4")
	d.Palette("git-unstage-lines")
	if !eventually(d, func() bool { return gitOut(d, "show", ":f.txt") == "l1\nX2\nl3\nl4\nl5\nl6" }) {
		t.Fatalf("index: %q\n%s", gitOut(d, "show", ":f.txt"), screen(d))
	}
	if got := diskFile(d, "f.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Errorf("the file on disk must not change: %q", got)
	}
}

func TestDiscardLineAsksAndCancelIsFirst(t *testing.T) {
	d := linesDriver(t)
	gotoLine(d, "4")
	d.Palette("git-discard-lines")
	if !d.WaitForText("Discard line 4?", 3*time.Second) {
		t.Fatalf("no card:\n%s", screen(d))
	}
	for _, l := range strings.Split(screen(d), "\n") {
		if c := strings.Index(l, "Cancel"); c >= 0 {
			if dsc := strings.Index(l, "Discard"); dsc < c {
				t.Errorf("Cancel is not first on the card row: %q", l)
			}
		}
	}
	// Enter alone keeps everything.
	d.Key("enter")
	d.Settle(0)
	if got := diskFile(d, "f.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Fatalf("Enter on a fresh card changed the file: %q", got)
	}
	// Esc keeps everything too.
	d.Palette("git-discard-lines")
	d.Key("esc")
	d.Settle(0)
	if got := diskFile(d, "f.txt"); got != "l1\nX2\nl3\nX4\nl5\nl6\n" {
		t.Fatalf("Esc changed the file: %q", got)
	}
}

func TestDiscardLineRevertsOnlyThatLine(t *testing.T) {
	d := linesDriver(t)
	gotoLine(d, "4")
	d.Palette("git-discard-lines")
	if !d.WaitForText("Discard line 4?", 3*time.Second) {
		t.Fatalf("no card:\n%s", screen(d))
	}
	clickButton(d, "Discard")
	if !eventually(d, func() bool { return diskFile(d, "f.txt") == "l1\nX2\nl3\nl4\nl5\nl6\n" }) {
		t.Fatalf("file on disk: %q", diskFile(d, "f.txt"))
	}
	if !eventually(d, func() bool { return !strings.Contains(bufLines(d), "X4") }) {
		t.Errorf("the open buffer still shows X4: %q", bufLines(d))
	}
	if got := gitOut(d, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("the index must not change: %q", got)
	}
}

// feed sends keys to nvim as typed, so that a visual selection stays.
func feed(d *Driver, keys string) {
	d.T.Helper()
	lua(d, `vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes(..., true, false, true), 'nx', false); return ''`, keys)
}

func TestStageSelectedLines(t *testing.T) {
	d := linesDriver(t)
	// Select lines 2 to 4 (a line-wise visual selection).
	feed(d, "2GV2j")
	d.Settle(0)
	d.Palette("git-stage-lines")
	if !eventually(d, func() bool { return gitOut(d, "show", ":f.txt") == "l1\nX2\nl3\nX4\nl5\nl6" }) {
		t.Fatalf("index: %q\n%s", gitOut(d, "show", ":f.txt"), screen(d))
	}
	if m := lua(d, `return vim.fn.mode()`); m != "n" {
		t.Errorf("the selection should end, mode %q", m)
	}
}

func TestStageSelectedLinesBackwards(t *testing.T) {
	d := linesDriver(t)
	// The cursor ends above the start of the selection: lines 4 up to 2.
	feed(d, "4GV2k")
	d.Settle(0)
	d.Palette("git-stage-lines")
	if !eventually(d, func() bool { return gitOut(d, "show", ":f.txt") == "l1\nX2\nl3\nX4\nl5\nl6" }) {
		t.Fatalf("index: %q", gitOut(d, "show", ":f.txt"))
	}
}

func TestStageLineRefusesUnsavedEdits(t *testing.T) {
	d := linesDriver(t)
	lua(d, `vim.api.nvim_buf_set_lines(0, 0, 1, false, {'typed'}); return ''`)
	d.Settle(0)
	gotoLine(d, "4")
	d.Palette("git-stage-lines")
	if !d.WaitForText("Save the file first", 3*time.Second) {
		t.Fatalf("no warning:\n%s", screen(d))
	}
	if got := gitOut(d, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("nothing may be staged: %q", got)
	}
}
