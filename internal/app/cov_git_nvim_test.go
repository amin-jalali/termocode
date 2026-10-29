package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// covNvimRepo is a temp repo whose model has a real headless nvim (closed
// when the test ends) with path rel open.
func covNvimRepo(t *testing.T, rel string) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	c := aiTestNvim(t, m)
	if err := c.ExecLua(conflictLua()); err != nil {
		t.Fatal(err)
	}
	covNvimOpen(t, m, filepath.Join(dir, rel))
	return m, dir
}

func covNvimOpen(t *testing.T, m *Model, path string) {
	t.Helper()
	if err := m.nvim.OpenFile("edit", path); err != nil {
		t.Fatal(err)
	}
	m.editor.SetMeta(path, false, "")
}

func covNvimEval(t *testing.T, m *Model, lua string) string {
	t.Helper()
	s, err := m.nvim.EvalLuaString(lua)
	if err != nil {
		t.Fatalf("lua: %v", err)
	}
	return s
}

func covNvimLine(t *testing.T, m *Model) string {
	return covNvimEval(t, m, `return tostring(vim.api.nvim_win_get_cursor(0)[1])`)
}

// With the editor running, a discard reloads the open file: the buffer
// shows the committed text again. Stage / discard lines take the cursor
// line and then move below it.
func TestCovGitEditorReloadsAfterDiscard(t *testing.T) {
	m, dir := covNvimRepo(t, "a.txt")
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	m.cursorLine = 2
	m.dispatchPaletteAction("git-discard-hunk")
	if got := covNvimEval(t, m, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), ',')`); got != "one,two,three" {
		t.Errorf("buffer after discard hunk = %q", got)
	}

	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	if err := m.nvim.Command("silent! checktime | 2"); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-stage-lines")
	if covGit(t, dir, "diff", "--cached", "--name-only") != "a.txt" || covNvimLine(t, m) != "3" {
		t.Fatalf("stage line: cursor %s", covNvimLine(t, m))
	}
	covGit(t, dir, "reset", "-q")
	if err := m.nvim.Command("2"); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-discard-lines")
	m.confirmOpen = false
	m.handleConfirmSelect("discard")
	if !toastHas(m, "Discarded line 2") {
		t.Error("no Discarded toast")
	}

	// Whole-file discard, stash, pop, apply, checkout: each reloads.
	covWrite(t, dir, "a.txt", "x\n")
	m.confirmTargetPath = "a.txt"
	m.confirmKind = confirmKindGitDiscard
	m.handleConfirmSelect("discard")
	covWrite(t, dir, "a.txt", "y\n")
	m.dispatchPaletteAction("git-stash")
	m.handlePickerSelect(picker.SelectMsg{ID: "stash@{0}"})
	covGit(t, dir, "checkout", "--", "a.txt")
	m.pickerKind = pickerKindStash
	m.dispatchPaletteAction("git-stash-pop")
	covGit(t, dir, "checkout", "--", "a.txt")
	covGit(t, dir, "tag", "v1")
	covGit(t, dir, "branch", "b2")
	m.pickerKind = pickerKindTags
	m.handlePickerSelect(picker.SelectMsg{ID: "tag-v1"})
	m.pickerKind = pickerKindBranches
	m.handlePickerSelect(picker.SelectMsg{ID: "co-b2"})
	m.gitRemoteDone(gitRemoteDoneMsg{okText: "Pulled", reload: true})
	if got := covNvimEval(t, m, `return vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]`); got != "one" {
		t.Errorf("buffer not reloaded: %q", got)
	}
	m.dispatchPaletteAction("reload-buffer")
	if !toastHas(m, "Buffer reloaded from disk") {
		t.Error("no reload toast")
	}
}

// The side-by-side diff and the commit diff open in the editor.
func TestCovGitEditorDiffViews(t *testing.T) {
	m, dir := covNvimRepo(t, "a.txt")
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covRefresh(m)
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowFile })
	cmd, _ := covKey(m, covRunes("d"))
	m.openSideBySideDiff(covMsg[gitDiffReadyMsg](t, cmd))
	if !m.gitDiffActive || m.focus != FocusEditor || m.err != "" {
		t.Fatalf("side-by-side diff not open: err=%q", m.err)
	}
	if n := covNvimEval(t, m, `return tostring(#vim.api.nvim_list_wins())`); n != "2" {
		t.Errorf("diff windows = %s", n)
	}
	hash := covGit(t, dir, "rev-parse", "--short", "HEAD")
	m.openCommitDiffBuffer(covMsg[gitCommitDiffReadyMsg](t, m.gitShowCommitCmd(hash)))
	if m.gitDiffActive || covNvimEval(t, m, `return vim.bo.filetype`) != "diff" {
		t.Error("commit diff buffer not open")
	}
}

// Merge conflicts in the editor: the code actions offer the three
// Accept actions; Accept resolves the block and counts the rest; ]x with
// none left says so; Enter on a CONFLICTS row opens the file at the
// first marker.
func TestCovGitEditorConflicts(t *testing.T) {
	m, dir := covNvimRepo(t, "a.txt")
	covGit(t, dir, "checkout", "-q", "-b", "side")
	covWrite(t, dir, "a.txt", "one\nSIDE\nthree\nfour\n")
	covCommit(t, dir, "side")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "a.txt", "one\nMAIN\nthree\n")
	covCommit(t, dir, "main")
	if out, err := covGitCmd(dir, "merge", "-q", "side").CombinedOutput(); err == nil {
		t.Fatalf("merge did not conflict: %s", out)
	}
	covRefresh(m)
	covCursorTo(t, m, func(r gitPanelRow) bool { return r.kind == gitRowFile && r.section == gitSecConflicts })
	covKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if covNvimLine(t, m) != "2" || m.focus != FocusEditor {
		t.Fatalf("conflict file not opened at the marker: line %s", covNvimLine(t, m))
	}

	m.cursorLine = 3
	var titles []string
	for _, a := range m.extraCodeActionsForContext() {
		titles = append(titles, a.Title)
	}
	if !strings.Contains(strings.Join(titles, "|"), "Accept Incoming Change (side)") {
		t.Errorf("code actions %v", titles)
	}
	m.cursorLine = 1
	for _, a := range m.extraCodeActionsForContext() {
		if strings.HasPrefix(a.Title, "Accept") {
			t.Errorf("Accept offered outside the block: %q", a.Title)
		}
	}

	if err := m.nvim.Command("3"); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-conflict-theirs")
	if !toastHas(m, "Accepted incoming change") || !toastHas(m, "No conflicts left") {
		t.Error("no accept toast")
	}
	got := covNvimEval(t, m, `return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), ',')`)
	if got != "one,SIDE,three,four" {
		t.Errorf("buffer after accept = %q", got)
	}
	m.dispatchPaletteAction("git-conflict-ours")
	if !toastHas(m, "No merge conflict at cursor") {
		t.Error("no note outside a conflict")
	}
	m.dispatchPaletteAction("git-conflict-next")
	if !toastHas(m, "No merge conflicts in this file") {
		t.Error("no note when none is left")
	}

	// Two blocks: one left after the first.
	text := "<<<<<<< HEAD\na\n=======\nb\n>>>>>>> x\nmid\n<<<<<<< HEAD\nc\n=======\nd\n>>>>>>> x\n"
	covNvimOpen(t, m, covWrite(t, dir, "two.txt", text))
	m.dispatchPaletteAction("git-conflict-next")
	if covNvimLine(t, m) != "7" && covNvimLine(t, m) != "1" {
		t.Errorf("]x went to line %s", covNvimLine(t, m))
	}
	if err := m.nvim.Command("2"); err != nil {
		t.Fatal(err)
	}
	m.dispatchPaletteAction("git-conflict-both")
	if !toastHas(m, "1 conflict left") {
		t.Error("no count of the conflicts left")
	}
}

// Next / Previous Change follow the git signs and wrap around; a buffer
// without signs stays put.
func TestCovGitEditorHunkJumps(t *testing.T) {
	m, _ := covNvimRepo(t, "a.txt")
	m.dispatchPaletteAction("next-hunk")
	if covNvimLine(t, m) != "1" {
		t.Fatal("moved with no signs")
	}
	covNvimEval(t, m, `
		vim.fn.sign_define('CovTest', { text = '+' })
		for _, l in ipairs({ 2, 3 }) do
			vim.fn.sign_place(0, 'TermocodeGitSigns', 'CovTest', vim.api.nvim_get_current_buf(), { lnum = l })
		end
		return ''`)
	m.dispatchPaletteAction("next-hunk")
	if covNvimLine(t, m) != "2" {
		t.Fatalf("next hunk at %s", covNvimLine(t, m))
	}
	m.dispatchPaletteAction("next-hunk") // wraps to the only hunk
	m.dispatchPaletteAction("prev-hunk")
	if covNvimLine(t, m) != "2" {
		t.Fatalf("prev hunk at %s", covNvimLine(t, m))
	}
}

// Open URL: the first link of the line opens in the browser; no link or
// a failing browser says so.
func TestCovGitEditorOpenURL(t *testing.T) {
	m, dir := covNvimRepo(t, "a.txt")
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()

	m.dispatchPaletteAction("open-url")
	if !toastHas(m, "No URL on this line") {
		t.Error("no note without a URL")
	}
	covNvimOpen(t, m, covWrite(t, dir, "links.md", "see (https://example.com/a?b=1) and http://x.y\n"))
	m.dispatchPaletteAction("open-url")
	if len(opened) != 1 || opened[0] != "https://example.com/a?b=1" || !toastHas(m, "Opened") {
		t.Fatalf("opened %v", opened)
	}
	osOpen = func(string) error { return errors.New("no browser") }
	m.dispatchPaletteAction("open-url")
	if !toastHas(m, "Open failed") {
		t.Error("no failure toast")
	}
	if _, err := os.Stat(filepath.Join(dir, "links.md")); err != nil {
		t.Fatal(err)
	}
}
