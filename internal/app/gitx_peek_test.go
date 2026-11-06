package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// peekModel is the editor on f.txt (ten lines, committed) in a real nvim.
func peekModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, dir := covGitRepo(t)
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	covWrite(t, dir, "f.txt", b.String())
	covCommit(t, dir, "ten")
	aiTestNvim(t, m)
	m.nvimAttached = true
	m.w, m.h = 120, 40
	covNvimOpen(t, m, filepath.Join(dir, "f.txt"))
	return m, dir
}

func peekPlain(m *Model) string { return stripANSI(m.confirm.View()) }

// peekAnswer presses a button of the open card (the card closes first, as
// a real click does).
func peekAnswer(m *Model, id string) {
	m.confirmOpen = false
	m.handleConfirmSelect(id)
}

// GITX-21: a click on the git mark in the gutter sends the clicked line
// to the app (termocode_git_peek); a click on a line without a mark
// sends nothing.
func TestGitxPeekGutterClick(t *testing.T) {
	m, _ := peekModel(t)
	if err := m.nvim.ExecLua(gitSignsLua); err != nil {
		t.Fatal(err)
	}
	covNvimEval(t, m, `
vim.o.mouse = 'a'
vim.o.number = true
vim.wo.statuscolumn = '` + editorStatusColumn + `'
_G._tc_git_marks[vim.api.nvim_get_current_buf()] = { [3] = 'TermocodeGitChange' }
_G._peek_got = nil
_G.termocode_notify = function(method, line) _G._peek_got = method .. ':' .. tostring(line) end
vim.cmd('redraw!')
return ''`)
	off := covNvimEval(t, m, `return tostring(vim.fn.getwininfo(vim.api.nvim_get_current_win())[1].textoff)`)
	var col int
	fmt.Sscanf(off, "%d", &col)
	click := func(row int) string {
		_ = m.nvim.Mouse("left", "press", "", row, col-1)
		_ = m.nvim.Mouse("left", "release", "", row, col-1)
		got := ""
		for end := time.Now().Add(2 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if got = covNvimEval(t, m, `return tostring(_G._peek_got or '')`); got != "" {
				break
			}
		}
		covNvimEval(t, m, `_G._peek_got = nil; return ''`)
		return got
	}
	if got := click(2); got != "termocode_git_peek:3" {
		t.Fatalf("click on the mark of line 3 sent %q", got)
	}
	if got := click(4); got != "" {
		t.Fatalf("click on a line without a mark sent %q", got)
	}
}

// The message opens the peek card: the change in colour, Close first and
// focused, Stage puts the change in the index, and the card on a staged
// change offers Unstage.
func TestGitxPeekStage(t *testing.T) {
	m, dir := peekModel(t)
	covWrite(t, dir, "f.txt", "l1\nl2\nL3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
	m.dispatchNotify(nvim.NotifyMsg{Method: "termocode_git_peek", Args: []any{int64(3)}})
	if !m.confirmOpen || m.confirmKind != confirmKindGitx || m.confirm.SafeID() != "close" {
		t.Fatalf("no peek card (open %v kind %v safe %q)", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	view := peekPlain(m)
	for _, want := range []string{"-l3", "+L3", "f.txt", "Close", "Stage", "Revert", "1 of 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("peek misses %q\n%s", want, view)
		}
	}
	peekAnswer(m, "stage")
	if !strings.Contains(covGit(t, dir, "diff", "--cached"), "+L3") || !toastHas(m, "Staged") {
		t.Fatal("Stage did not stage the change")
	}
	m.cursorLine = 3
	m.dispatchPaletteAction("git-peek-change")
	if !m.confirmOpen || !strings.Contains(peekPlain(m), "Unstage") {
		t.Fatalf("no Unstage on a staged change\n%s", peekPlain(m))
	}
	peekAnswer(m, "unstage")
	if covGit(t, dir, "diff", "--cached") != "" {
		t.Fatal("Unstage left the change staged")
	}
	if _, ok := aiActionPolicies["git-peek-change"]; !ok {
		t.Error("git-peek-change has no AI policy")
	}
}

// Revert asks first on a Cancel-first card: Cancel keeps the change,
// Revert puts the committed text back in the file and the buffer.
func TestGitxPeekRevert(t *testing.T) {
	m, dir := peekModel(t)
	covWrite(t, dir, "f.txt", "l1\nl2\nL3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")
	m.gitPeekAt(3)
	peekAnswer(m, "revert")
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" || !strings.Contains(peekPlain(m), "cannot be undone") {
		t.Fatalf("Revert did not ask first\n%s", peekPlain(m))
	}
	peekAnswer(m, "cancel")
	if !strings.Contains(covGit(t, dir, "diff"), "+L3") {
		t.Fatal("Cancel reverted the change")
	}
	m.gitPeekAt(3)
	peekAnswer(m, "revert")
	peekAnswer(m, "revert")
	if covGit(t, dir, "diff") != "" || !toastHas(m, "Reverted") {
		t.Fatal("Revert left the change")
	}
	if got := covNvimEval(t, m, `return vim.api.nvim_buf_get_lines(0, 2, 3, false)[1]`); got != "l3" {
		t.Errorf("buffer line 3 = %q after revert", got)
	}
}

// Next goes to the next change (and wraps): the cursor moves and the
// card shows that change; a clean line and an unsaved buffer say why
// there is no card.
func TestGitxPeekNextAndEdges(t *testing.T) {
	m, dir := peekModel(t)
	covWrite(t, dir, "f.txt", "l1\nL2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nL10\n")
	m.gitPeekAt(2)
	if !strings.Contains(peekPlain(m), "1 of 2") {
		t.Fatalf("first card\n%s", peekPlain(m))
	}
	peekAnswer(m, "next")
	if !m.confirmOpen || !strings.Contains(peekPlain(m), "2 of 2") || !strings.Contains(peekPlain(m), "+L10") {
		t.Fatalf("Next did not show the second change\n%s", peekPlain(m))
	}
	if covNvimLine(t, m) != "10" {
		t.Errorf("cursor at %s after Next", covNvimLine(t, m))
	}
	peekAnswer(m, "next")
	if !strings.Contains(peekPlain(m), "1 of 2") {
		t.Fatalf("Next did not wrap\n%s", peekPlain(m))
	}
	peekAnswer(m, "close")
	if m.confirmOpen {
		t.Fatal("Close left the card open")
	}
	m.gitPeekAt(6)
	if m.confirmOpen || !toastHas(m, "No change on line 6") {
		t.Fatal("a clean line opened a card")
	}
	covNvimEval(t, m, `vim.api.nvim_buf_set_lines(0, 0, 1, false, {'edited'}); return ''`)
	m.gitPeekAt(2)
	if m.confirmOpen || !toastHas(m, "Save the file first") {
		t.Fatal("an unsaved buffer opened a card")
	}
}
