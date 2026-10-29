package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

type gitLineKind int

const (
	gitLineStage gitLineKind = iota
	gitLineUnstage
	gitLineDiscard
)

// gitStageLines stages the line under the cursor, or the lines of the visual
// selection, and nothing else: the single-line twin of gitStageHunk.
func (m *Model) gitStageLines() tea.Cmd { return m.gitLinesOp(gitLineStage) }

// gitUnstageLines takes the line under the cursor, or the selected lines,
// out of the index again.
func (m *Model) gitUnstageLines() tea.Cmd { return m.gitLinesOp(gitLineUnstage) }

// gitLineRange is the lines the next line operation works on: the visual
// selection of the editor, or just the cursor line. nvim is asked directly:
// the app's copy of the cursor may be a moment behind.
func (m *Model) gitLineRange() (from, to int) {
	from, to = m.cursorLine, m.cursorLine
	if m.nvim == nil {
		return from, to
	}
	s, err := m.nvim.EvalLuaString(`
local mode = vim.fn.mode()
local l = vim.fn.line('.')
if mode ~= 'v' and mode ~= 'V' and mode ~= '\22' then return l .. ',' .. l end
return vim.fn.line('v') .. ',' .. l`)
	if err != nil {
		return from, to
	}
	var a, b int
	if _, err := fmt.Sscanf(s, "%d,%d", &a, &b); err == nil && a > 0 && b > 0 {
		from, to = min(a, b), max(a, b)
	}
	return from, to
}

// gitLineTarget finds the open file as a path relative to the repository.
func (m *Model) gitLineTarget() (cwd, rel string, ok bool) {
	path := m.editor.Path()
	if path == "" {
		return "", "", false
	}
	cwd, err := m.rootDir()
	if err != nil {
		return "", "", false
	}
	rel = path
	if r, e := filepath.Rel(cwd, path); e == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	return cwd, rel, true
}

func gitLineLabel(from, to int) string {
	if from == to {
		return fmt.Sprintf("line %d", from)
	}
	return fmt.Sprintf("lines %d-%d", from, to)
}

func (m *Model) gitLinesOp(kind gitLineKind) tea.Cmd {
	cwd, rel, ok := m.gitLineTarget()
	if !ok {
		return nil
	}
	var toastCmd tea.Cmd
	// The line numbers are those of the file on disk: unsaved edits shift them.
	if b, ok := m.liveBuffer(m.currentBufferID()); ok && b.Modified {
		m.toast, toastCmd = m.toast.PushDetail(toast.Warn, "Save the file first", "Lines are cut from the saved file")
		return toastCmd
	}
	from, to := m.gitLineRange()
	var opErr error
	var verb string
	switch kind {
	case gitLineStage:
		opErr, verb = git.StageLines(cwd, rel, from, to), "Staged "+gitLineLabel(from, to)
	case gitLineUnstage:
		opErr, verb = git.UnstageLines(cwd, rel, from, to), "Unstaged "+gitLineLabel(from, to)
	}
	m.leaveVisualMode()
	if opErr != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Git", opErr.Error())
		return toastCmd
	}
	m.gotoLineAfter(to)
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, verb, filepath.Base(rel))
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// leaveVisualMode ends a visual selection after an operation used it.
func (m *Model) leaveVisualMode() {
	if m.nvim == nil {
		return
	}
	_, _ = m.nvim.EvalLuaString(`
local mode = vim.fn.mode()
if mode == 'v' or mode == 'V' or mode == '\22' then
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('<Esc>', true, false, true), 'nx', false)
end
return ''`)
}

// gotoLineAfter puts the cursor on the line after n, so that pressing the
// key again takes the next line.
func (m *Model) gotoLineAfter(n int) {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.Command(fmt.Sprintf("silent! %d", n+1))
}
