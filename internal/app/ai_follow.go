package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Follow Agent (CMP-68): while it is on, every file change the agent
// writes (write_file, edit_file, through aiWriteFile) opens in the editor
// at the first changed line, so the user can watch the work without
// hunting for it. The chat keeps the keyboard focus.
//
// It never fights the user: while the user types in the editor or the
// terminal, follow pauses (no jump) and picks up again at the next edit
// once the user is back in the chat or another panel. It is off by
// default and lasts for the session (AI: Toggle Follow Agent).

// aiFollowJumpLua opens path in the current (editor) window at line and
// returns the buffer name it shows afterwards. 'edit' fails on a hidden
// modified buffer when 'hidden' is off; pcall keeps the user's tab then.
const aiFollowJumpLua = `local p, l = ...
pcall(vim.cmd, 'edit ' .. vim.fn.fnameescape(p))
if vim.api.nvim_buf_get_name(0) == p then
  local n = vim.api.nvim_buf_line_count(0)
  pcall(vim.api.nvim_win_set_cursor, 0, { math.min(math.max(l, 1), n), 0 })
  pcall(vim.cmd, 'normal! zz')
end
return vim.api.nvim_buf_get_name(0)`

// aiFirstChangedLine is the 1-based line where after starts to differ
// from before (0: same text). A pure append points at the first new line.
func aiFirstChangedLine(before, after string) int {
	if before == after {
		return 0
	}
	a := strings.Split(before, "\n")
	b := strings.Split(after, "\n")
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i + 1
		}
	}
	return n
}

// aiFollowPaused reports whether the user is busy where a jump would get
// in the way: typing in the editor or in the terminal.
func (m *Model) aiFollowPaused() bool {
	if m.inTerminal {
		return true
	}
	return m.focus == FocusEditor && !m.aiS().focused && !m.panelFocused
}

// aiFollowJump moves the editor to abs:line when Follow Agent is on and
// returns a note for the model ("" when it did not jump).
func (m *Model) aiFollowJump(abs, rel string, line int) string {
	s := m.aiS()
	if !s.follow || m.nvim == nil || line <= 0 || m.aiFollowPaused() {
		return ""
	}
	m.ensureEditorWindowCurrent()
	var name string
	if err := m.nvim.ExecLuaResult(aiFollowJumpLua, &name, abs, line); err != nil || filepath.Clean(name) != abs {
		return ""
	}
	if b := m.aiBatchNow(); b != nil {
		b.have = false // a new buffer may be open now: read the list again
	}
	m.fetchStateSoon()
	m.explorer.RevealPath(abs)
	s.followAt = fmt.Sprintf("%s:%d", rel, line)
	return fmt.Sprintf("; Follow Agent: the editor shows %s at line %d (the user watches it)", rel, line)
}

// aiToggleFollow is AI: Toggle Follow Agent.
func (m *Model) aiToggleFollow() tea.Cmd {
	s := m.aiS()
	s.follow = !s.follow
	s.followAt = ""
	label := "Follow Agent off: the editor stays where you leave it"
	if s.follow {
		label = "Follow Agent on: the editor jumps to each AI edit (paused while you type in the editor)"
	}
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, label)
	return c
}

// aiFollowStatusTexts is the status-bar mark while Follow Agent is on.
func (m Model) aiFollowStatusTexts() []string {
	if m.ai == nil || !m.ai.follow {
		return nil
	}
	g := "◎"
	if theme.CurrentIconMode == theme.IconModeASCII {
		g = "(o)"
	}
	return []string{"{{accent}}" + g + "{{/}} Following AI"}
}
