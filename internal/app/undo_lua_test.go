package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// undoNvim is a real nvim with persistentUndoLua, its data dir (and so
// the undo dir) in a temp dir.
func undoNvim(t *testing.T) *nvim.Client {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c := newRestoreNvim(t)
	if err := c.ExecLua(persistentUndoLua); err != nil {
		t.Fatal(err)
	}
	return c
}

func undoEval(t *testing.T, c *nvim.Client, lua string) string {
	t.Helper()
	s, err := c.EvalLuaString(lua)
	if err != nil {
		t.Fatalf("%s: %v", lua, err)
	}
	return s
}

// Spec: Given a file with no undo history on disk,
// when it is opened,
// then nvim does not hash the whole file for the undo file check
// ('undofile' stays off for the buffer while it is read; it opened a
// 50k-line file 10 ms slower).
func TestUndoFileOffWhileReadingFileWithoutHistory(t *testing.T) {
	c := undoNvim(t)
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	if got := undoEval(t, c, `return tostring(vim.bo.undofile)`); got != "false" {
		t.Fatalf("undofile = %s after opening a file with no undo history, want false", got)
	}
}

// Spec: Given a file opened with no undo history,
// when it is edited, saved, closed and opened again,
// then undo still brings the old text back (the history was written on
// save and is read on the next open).
func TestUndoHistorySurvivesReopen(t *testing.T) {
	c := undoNvim(t)
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, 1, false, { 'ONE' }); vim.cmd('silent write')`); err != nil {
		t.Fatal(err)
	}
	if got := undoEval(t, c, `return tostring(vim.uv.fs_stat(vim.fn.undofile(vim.api.nvim_buf_get_name(0))) ~= nil)`); got != "true" {
		t.Fatal("save wrote no undo file")
	}
	if err := c.ExecLua(`vim.cmd('enew'); vim.cmd('bwipeout! #')`); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	if got := undoEval(t, c, `return tostring(vim.bo.undofile)`); got != "true" {
		t.Fatalf("undofile = %s on a file with undo history, want true", got)
	}
	if err := c.ExecLua(`vim.cmd('silent undo')`); err != nil {
		t.Fatal(err)
	}
	if got := undoEval(t, c, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), ',')`); !strings.HasPrefix(got, "one,") {
		t.Fatalf("after reopen + undo the text is %q, want the old first line back", got)
	}
}
