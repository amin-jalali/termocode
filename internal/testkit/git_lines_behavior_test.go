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
