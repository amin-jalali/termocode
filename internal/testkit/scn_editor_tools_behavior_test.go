package testkit

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// SCN-16: editor tools end to end — bookmarks, user snippets, format on
// save when the formatter fails, saving a read-only or CRLF file, sort and
// case commands, and folds that survive a tab switch. Multi-cursor needs
// the vim-visual-multi plugin from the network, so it stays covered by
// internal/app/multi_cursor_nvim_test.go.

// scnSet replaces the current buffer with lines and puts the cursor on
// row (1-based).
func scnSet(d *Driver, row int, lines ...string) {
	d.T.Helper()
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = "[==[" + l + "]==]"
	}
	lua(d, `vim.api.nvim_buf_set_lines(0, 0, -1, false, {`+strings.Join(parts, ",")+`})
		vim.api.nvim_win_set_cursor(0, {`+strconv.Itoa(row)+`, 0}) return ''`)
}

// markRow is the row of global mark A in the current buffer (0 = unset).
func markRow(d *Driver) string {
	d.T.Helper()
	return lua(d, `local p = vim.api.nvim_get_mark('A', {}) return tostring(p[1])`)
}

// Spec: Given a.txt open with the cursor on line 2,
// when the user runs Bookmarks: Toggle on Current Line,
// then line 2 holds a bookmark;
// when the user runs it again on the same line, the bookmark is gone;
// when the user sets it once more and runs Bookmarks: Clear All,
// then no bookmark is left.
func TestSCNEditorBookmarkToggleAndClear(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	lua(d, `vim.api.nvim_win_set_cursor(0, {2, 0}) return ''`)
	d.Palette("bookmark-toggle")
	if !eventually(d, func() bool { return markRow(d) == "2" }) {
		t.Fatalf("after toggle: bookmark row = %s, want 2", markRow(d))
	}
	d.Palette("bookmark-toggle")
	if !eventually(d, func() bool { return markRow(d) == "0" }) {
		t.Fatalf("second toggle on the same line: bookmark row = %s, want removed", markRow(d))
	}
	d.Palette("bookmark-toggle")
	if !eventually(d, func() bool { return markRow(d) == "2" }) {
		t.Fatalf("toggle again: bookmark row = %s, want 2", markRow(d))
	}
	d.Palette("bookmark-clear")
	if !eventually(d, func() bool { return markRow(d) == "0" }) {
		t.Errorf("after Clear All: bookmark row = %s, want none", markRow(d))
	}
}

// Spec: Given a user snippet "hdr" for every file type in snippets.json
// and a.txt open,
// when the user types "hdr" on an empty line and presses Tab,
// then the trigger is replaced by the snippet body.
func TestSCNEditorUserSnippetExpands(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Config: map[string]string{"snippets.json": `{"*": {"hdr": "== header =="}}`}})
	open(d, "a.txt")
	scnSet(d, 1, "", "alpha")
	d.Type("hdr")
	d.Key("tab")
	if !eventually(d, func() bool { return strings.HasPrefix(bufLines(d), "== header ==") }) {
		t.Fatalf("Tab after the trigger: buffer = %q, want the snippet body on line 1", bufLines(d))
	}
	if strings.Contains(bufLines(d), "hdr") {
		t.Errorf("trigger left in the buffer: %q", bufLines(d))
	}
}

// Spec: Given a.txt open and a formatter that fails (an LSP error),
// when the user edits and presses Ctrl+S,
// then the file is still written with the edit and the tab is clean:
// format on save never blocks a save.
func TestSCNEditorFormatOnSaveErrorStillSaves(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	lua(d, `vim.lsp.buf.format = function() error('formatter crashed') end return ''`)
	d.Type("X")
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tab not dirty after typing: %v", got)
	}
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return diskFile(d, "a.txt") == "Xalpha\nbravo\ncharlie\n" }) {
		t.Fatalf("after Ctrl+S with a failing formatter: file = %q", diskFile(d, "a.txt"))
	}
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Errorf("tab still dirty after the save: %v", got)
	}
}

// Spec: Given a read-only a.txt open,
// when the user edits and presses Ctrl+S,
// then the file on disk is unchanged, the tab stays dirty and the user
// is told the file is read-only.
func TestSCNEditorSaveReadOnlyKeepsEdit(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write a read-only file")
	}
	d := editorDriver(t, nil)
	p := filepath.Join(d.Dir, "a.txt")
	if err := os.Chmod(p, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	open(d, "a.txt")
	d.Type("X")
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tab not dirty after typing: %v", got)
	}
	d.Key("ctrl+s")
	d.Settle(300 * time.Millisecond)
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Fatalf("read-only file changed on disk: %q", got)
	}
	if got, _ := tabs(d); len(got) != 1 || got[0] != "a.txt*" {
		t.Errorf("tabs = %v, want a.txt still dirty (the edit is kept)", got)
	}
	if !d.WaitForText("read-only", 2*time.Second) && !strings.Contains(screen(d), "No permission") {
		t.Errorf("no read-only message shown:\n%s", screen(d))
	}
}

// Spec: Given win.txt with Windows line ends (CRLF) open,
// when the user edits and saves,
// then every line still ends in CRLF on disk.
func TestSCNEditorSaveKeepsCRLF(t *testing.T) {
	files := textFiles()
	files["win.txt"] = "one\r\ntwo\r\n"
	d := editorDriver(t, files)
	open(d, "win.txt")
	d.Type("X")
	if !waitTabs(d, "win.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tab not dirty after typing: %v", got)
	}
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return diskFile(d, "win.txt") == "Xone\r\ntwo\r\n" }) {
		t.Errorf("after save: file = %q, want CRLF kept", diskFile(d, "win.txt"))
	}
}

// Spec: Given a.txt holding "pear", "apple", "fig",
// when the user runs Sort Lines Ascending, the lines read apple, fig, pear;
// when the user runs Sort Lines Descending, they read pear, fig, apple;
// when the user runs Convert to UPPERCASE and then to lowercase, the text
// changes case and nothing else.
func TestSCNEditorSortAndCase(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	scnSet(d, 1, "pear", "apple", "fig")
	steps := []struct{ id, want string }{
		{"sort-asc", "apple\nfig\npear"},
		{"sort-desc", "pear\nfig\napple"},
		{"uppercase", "PEAR\nFIG\nAPPLE"},
		{"lowercase", "pear\nfig\napple"},
	}
	for _, s := range steps {
		d.Palette(s.id)
		if !eventually(d, func() bool { return bufLines(d) == s.want }) {
			t.Fatalf("after %s: buffer = %q, want %q", s.id, bufLines(d), s.want)
		}
	}
	if got, _ := tabs(d); len(got) != 1 || got[0] != "a.txt*" {
		t.Errorf("tabs = %v, want a.txt dirty (the edits are unsaved)", got)
	}
}

// Spec: Given a.txt open with lines 1-2 folded shut,
// when the user switches to b.txt and back to a.txt,
// then lines 1-2 are still folded.
func TestSCNEditorFoldSurvivesTabSwitch(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	got := lua(d, `vim.wo.foldmethod = 'manual' vim.cmd('1,2fold') return tostring(vim.fn.foldclosed(1))`)
	if got != "1" {
		t.Fatalf("fold not made: foldclosed(1) = %s", got)
	}
	open(d, "b.txt")
	open(d, "a.txt")
	if !eventually(d, func() bool { return lua(d, `return tostring(vim.fn.foldclosed(1))`) == "1" }) {
		t.Errorf("after a tab switch: foldclosed(1) = %s, want the fold kept shut",
			lua(d, `return tostring(vim.fn.foldclosed(1))`))
	}
}
