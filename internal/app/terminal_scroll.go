package app

import tea "github.com/charmbracelet/bubbletea"

// Terminal scrollback with the mouse wheel.
//
// nvim shows the scrollback only in Terminal-Normal mode (nt), and a
// ModeChanged autocmd (model.go) sends every nt straight back to
// Terminal-insert so a click in the panel never leaves the user typing
// normal-mode commands. That also undid every wheel notch: the wheel left
// insert mode, the autocmd re-entered it, the view snapped back to the
// prompt. While vim.g.termocode_term_scroll is set the autocmd lets nt
// stay, so the wheel scrolls; the next key or click in the app ends the
// scroll and returns to the prompt, as in VS Code.

// terminalWheel forwards a wheel notch over the terminal split and keeps
// nvim in scrollback mode.
func (m Model) terminalWheel(localX, localY int, t tea.MouseEventType) (tea.Model, tea.Cmd) {
	if m.nvim != nil && !m.termScrolled {
		m.termScrolled = true
		_ = m.nvim.ExecLua(`vim.g.termocode_term_scroll = true`)
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.HandleMouse(localX, localY, t)
	return m, cmd
}

// endTerminalScroll leaves scrollback mode: the terminal (when it is the
// current window) goes back to Terminal-insert at the prompt, and keys
// pass to the shell again at once.
func (m *Model) endTerminalScroll() {
	if !m.termScrolled {
		return
	}
	m.termScrolled = false
	if m.nvim == nil {
		return
	}
	back, _ := m.nvim.EvalLuaString(`
		vim.g.termocode_term_scroll = false
		if vim.bo.buftype == 'terminal' then
			vim.cmd('startinsert')
			return vim.api.nvim_get_current_win() == ` + itoa(m.terminalWinID) + ` and 'yes' or ''
		end
		return ''`)
	if back == "yes" && m.panelActive == panelKindTerminal {
		m.inTerminal = true
	}
}
