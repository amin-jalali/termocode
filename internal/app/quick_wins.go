package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// sortLinesAsc / sortLinesDesc / reverseLines / uppercaseSelection /
// lowercaseSelection / titleCaseSelection are buffer-modifying commands
// that all delegate to nvim's built-in Ex commands. We bundle them here
// because they share the same shape: a one-liner Ex command + a toast.
//
// Each operates on the visual selection if one is active, falling back to
// the whole buffer when invoked from Normal/Insert. nvim's `:sort` etc.
// already respect range markers (`'<,'>`) when in Visual mode and default
// to whole-file otherwise, so the same command string works in both
// contexts.

func (m *Model) sortLinesAsc() tea.Cmd {
	return m.runEditorCmd(`silent! '<,'>sort`, `silent! sort`, "Sorted lines (ascending)")
}

func (m *Model) sortLinesDesc() tea.Cmd {
	return m.runEditorCmd(`silent! '<,'>sort!`, `silent! sort!`, "Sorted lines (descending)")
}

func (m *Model) reverseLines() tea.Cmd {
	// `:g/^/m 0` is the canonical vim trick for reversing a buffer's lines.
	// In a visual range it reverses just the selection.
	return m.runEditorCmd(`silent! '<,'>g/^/m '<-1`, `silent! g/^/m 0`, "Reversed lines")
}

func (m *Model) uppercaseSelection() tea.Cmd {
	return m.runEditorCmd(`silent! '<,'>s/.*/\U&/`, `silent! %s/.*/\U&/`, "UPPERCASED")
}

func (m *Model) lowercaseSelection() tea.Cmd {
	return m.runEditorCmd(`silent! '<,'>s/.*/\L&/`, `silent! %s/.*/\L&/`, "lowercased")
}

// runEditorCmd picks `visualForm` vs `wholeBufForm` based on the live nvim
// mode and dispatches via a single ExecLua chunk. Returns a toast cmd.
func (m *Model) runEditorCmd(visualForm, wholeForm, successMsg string) tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	mode, _ := m.nvim.EvalLuaString(`return vim.api.nvim_get_mode().mode:sub(1,1)`)
	cmd := wholeForm
	if mode == "v" || mode == "V" || mode == "\x16" {
		cmd = visualForm
	}
	_ = m.nvim.Command(cmd)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, successMsg)
	return toastCmd
}

// joinLines runs `J` in normal mode (or visual) — joins the current line
// with the next, removing trailing whitespace. Convenient palette entry
// because the user has to drop to Normal mode to use the J keystroke.
func (m *Model) joinLines() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	_ = m.nvim.Command("normal! J")
	return nil
}

// gotoNextDiagnostic / gotoPrevDiagnostic walk the LSP diagnostic stream in
// the current buffer. Mapped to F5 / Shift+F5 (palette only by default).
func (m *Model) gotoNextDiagnostic() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(`pcall(vim.diagnostic.goto_next)`)
}

func (m *Model) gotoPrevDiagnostic() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.ExecLua(`pcall(vim.diagnostic.goto_prev)`)
}

// toggleHiddenFiles flips whether the file explorer shows dotfiles.
// Uses the explorer's existing API; mirrors VSCode's "Files: Toggle Excluded
// Files" behaviour.
func (m *Model) toggleHiddenFiles() tea.Cmd {
	m.explorer.ToggleHidden()
	var toastCmd tea.Cmd
	if m.explorer.HiddenShown() {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Hidden files: shown")
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Hidden files: hidden")
	}
	return toastCmd
}

// alternateFile swaps to the previous buffer (vim's `:b#` / Ctrl+^).
// Bound to Ctrl+Tab so users can flip between two files at speed.
func (m *Model) alternateFile() {
	if m.nvim == nil {
		return
	}
	_ = m.nvim.Command("buffer #")
}
