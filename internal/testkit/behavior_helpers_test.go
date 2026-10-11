package testkit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Helpers for the behaviour tests (*_behavior_test.go): read the screen,
// the files on disk and the editor state, and find things to click.

// textFiles is a small workspace without source code, so no language
// server starts and nvim tests stay quick.
func textFiles() map[string]string {
	return map[string]string{
		"a.txt":         "alpha\nbravo\ncharlie\n",
		"b.txt":         "one\ntwo\nthree\n",
		"c.txt":         "red\ngreen\nblue\n",
		"notes/todo.md": "# Todo\n\n- milk\n",
		"notes/done.md": "# Done\n",
	}
}

// editorDriver starts the app with a real nvim on the text workspace.
// It is skipped with -short (each one starts nvim).
func editorDriver(t *testing.T, files map[string]string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	if files == nil {
		files = textFiles()
	}
	return New(t, Options{W: 120, H: 34, Nvim: true, Files: files})
}

// screen is the last frame without colours.
func screen(d *Driver) string { return ansi.Strip(d.Frame()) }

// diskFile reads a workspace file ("" when it does not exist).
func diskFile(d *Driver, rel string) string {
	b, err := os.ReadFile(filepath.Join(d.Dir, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// eventually waits (up to 3 s) until cond holds: file operations finish
// in commands that may still run when a step has settled.
func eventually(d *Driver, cond func() bool) bool {
	d.T.Helper()
	return d.WaitFor(cond, 3*time.Second)
}

func exists(d *Driver, rel string) bool {
	_, err := os.Stat(filepath.Join(d.Dir, rel))
	return err == nil
}

// lua runs a Lua chunk in the app's nvim and returns its string result.
func lua(d *Driver, code string, args ...any) string {
	d.T.Helper()
	s, err := d.Model().NvimLua(code, args...)
	if err != nil {
		d.T.Fatalf("lua %q: %v", code, err)
	}
	return s
}

// bufLines is the text of the current nvim buffer.
func bufLines(d *Driver) string {
	d.T.Helper()
	return lua(d, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), "\n")`)
}

// cursor is the 1-based "line:col" of the cursor in the current window.
func cursor(d *Driver) string {
	d.T.Helper()
	return lua(d, `local p = vim.api.nvim_win_get_cursor(0); return p[1] .. ":" .. (p[2] + 1)`)
}

// tabs returns the open editor tabs as workspace-relative paths (in
// tab-bar order, with "*" after a modified one) and the active one.
func tabs(d *Driver) ([]string, string) {
	bufs, active := d.Model().OpenBuffers()
	var out []string
	var act string
	for _, b := range bufs {
		if b.Path == "" { // the Welcome tab
			continue
		}
		rel := relPath(d, b.Path)
		if b.ID == active {
			act = rel
		}
		if b.Modified {
			rel += "*"
		}
		out = append(out, rel)
	}
	return out, act
}

func relPath(d *Driver, p string) string {
	if r, err := filepath.Rel(d.Dir, p); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return p
}

// open opens a workspace file in the editor and waits until it is the
// active tab.
func open(d *Driver, rel string) {
	d.T.Helper()
	d.Action("open:" + filepath.Join(d.Dir, rel))
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == rel }, 5*time.Second) {
		got, a := tabs(d)
		nv, _ := d.Model().NvimLua(`return vim.api.nvim_buf_get_name(0) .. ' ' .. vim.fn.mode()`)
		d.T.Fatalf("open %s: active tab %q, tabs %v; nvim: %s\n%s", rel, a, got, nv, screen(d))
	}
}

// waitTabs waits until the tab list equals want.
func waitTabs(d *Driver, want ...string) bool {
	d.T.Helper()
	return d.WaitFor(func() bool { got, _ := tabs(d); return slices.Equal(got, want) }, 5*time.Second)
}

// find returns the screen cell (x, y) where text first appears in the
// frame, searching rows from the top; ok is false when it is not shown.
func find(d *Driver, text string) (x, y int, ok bool) {
	for i, line := range strings.Split(screen(d), "\n") {
		if j := strings.Index(line, text); j >= 0 {
			return ansi.StringWidth(line[:j]), i, true
		}
	}
	return 0, 0, false
}

// findIn is find limited to the columns [x0, x1).
func findIn(d *Driver, text string, x0, x1 int) (x, y int, ok bool) {
	for i, line := range strings.Split(screen(d), "\n") {
		cut := ansi.Cut(line, x0, x1)
		if j := strings.Index(cut, text); j >= 0 {
			return x0 + ansi.StringWidth(cut[:j]), i, true
		}
	}
	return 0, 0, false
}

// clickText clicks the first place text is shown.
func clickText(d *Driver, text string) {
	d.T.Helper()
	x, y, ok := find(d, text)
	if !ok {
		d.T.Fatalf("%q not on screen:\n%s", text, screen(d))
	}
	d.Click(x+1, y)
}

// clickButton clicks a dialog button: label on the row that also shows
// the Cancel button (so a title with the same word is not hit).
func clickButton(d *Driver, label string) {
	d.T.Helper()
	for y, line := range strings.Split(screen(d), "\n") {
		if !strings.Contains(line, "Cancel") {
			continue
		}
		if j := strings.Index(line, " "+label+" "); j >= 0 {
			d.Click(ansi.StringWidth(line[:j])+2, y)
			return
		}
	}
	d.T.Fatalf("no %q button on screen:\n%s", label, screen(d))
}

// sidebarW is the width of activity bar + explorer in the default layout.
const sidebarW = 33

// explorerRow returns the row y where the explorer lists name.
func explorerRow(d *Driver, name string) (int, bool) {
	_, y, ok := findIn(d, " "+name, 0, sidebarW)
	return y, ok
}

// rightClickExplorer opens the context menu of an explorer row.
func rightClickExplorer(d *Driver, name string) {
	d.T.Helper()
	y, ok := explorerRow(d, name)
	if !ok {
		d.T.Fatalf("explorer does not show %q:\n%s", name, screen(d))
	}
	d.RightClick(10, y)
}

// clickExplorer left-clicks an explorer row.
func clickExplorer(d *Driver, name string) {
	d.T.Helper()
	y, ok := explorerRow(d, name)
	if !ok {
		d.T.Fatalf("explorer does not show %q:\n%s", name, screen(d))
	}
	d.Click(10, y)
}

// explorerShows reports whether the explorer column lists name.
func explorerShows(d *Driver, name string) bool {
	_, ok := explorerRow(d, name)
	return ok
}
