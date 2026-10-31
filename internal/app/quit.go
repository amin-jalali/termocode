package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
)

// quit ends the app. With unsaved files it first asks Save All / Don't
// Save / Cancel: files are not saved behind the user's back (no save on
// buffer switch unless auto_save is on), so quitting must not drop edits
// silently either.
func (m *Model) quit() tea.Cmd {
	if names := m.unsavedFiles(); len(names) > 0 {
		m.promptQuit(names)
		return nil
	}
	return m.quitNow()
}

// quitNow closes nvim and quits without asking.
func (m *Model) quitNow() tea.Cmd {
	if c, wait := m.databasesBeforeQuit(); wait { // uncommitted changes (db_tx.go)
		return c
	}
	if c, wait := m.servicesBeforeQuit(); wait { // services_quit.go
		return c
	}
	m.stopMCPServe() // outside agents get EOF; waiting cards are a no
	m.stopWebHub()   // the web gateway gets EOF (web_run.go)
	m.removeAgentMCPFiles()
	m.agentRollupRemove() // OMX-35
	m.observeTerminals()  // what each shell runs now, before the shells end
	if m.nvim != nil {
		_ = m.nvim.Close()
	}
	m.sess.flush()
	m.dropHotExitBackup()
	return tea.Quit
}

// unsavedFiles lists the modified file buffers, asked from nvim (the
// tab bar's copy of the modified flags can lag a redraw behind).
func (m *Model) unsavedFiles() []string {
	if m.nvim == nil {
		return nil
	}
	var names []string
	err := m.nvim.EvalLuaArgs(`local out = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].buflisted and vim.bo[b].buftype == '' and vim.bo[b].modified then
    local name = vim.api.nvim_buf_get_name(b)
    if name ~= '' then out[#out + 1] = name end
  end
end
return out`, &names)
	if err != nil {
		return nil
	}
	return names
}

// promptQuit opens the unsaved-changes question for quitting.
func (m *Model) promptQuit(paths []string) {
	base := make([]string, 0, len(paths))
	for _, p := range paths {
		base = append(base, filepath.Base(p))
	}
	body := fmt.Sprintf("%q has unsaved changes.", base[0])
	if len(base) > 1 {
		shown := base
		if len(shown) > 3 {
			shown = append(shown[:3:3], "…")
		}
		body = fmt.Sprintf("%d files have unsaved changes: %s.", len(base), strings.Join(shown, ", "))
	}
	m.showQuitCard(body)
}

// showQuitCard opens the Save All / Don't Save / Cancel card with body.
func (m *Model) showQuitCard(body string) {
	m.confirm = confirm.New(
		"Save changes before quitting?",
		body,
		[]confirm.Button{
			{ID: "save", Title: "Save All", Style: confirm.StylePrimary},
			{ID: "discard", Title: "Don't Save", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindQuit
}

// onQuitConfirm handles the quit question's buttons.
func (m *Model) onQuitConfirm(id string) tea.Cmd {
	switch id {
	case "save":
		if m.askSaveConflict(0, saveAndQuit) {
			return nil
		}
		if m.nvim != nil {
			_ = m.nvim.Command("silent! wall")
		}
		// A file that could not be written (read-only, deleted folder)
		// is still modified: ask again instead of losing it.
		if left := m.unsavedFiles(); len(left) > 0 {
			m.showQuitCard(saveAllFailText(left)) // say why it asks again
			return nil
		}
		return m.quitNow()
	case "discard":
		return m.quitNow()
	}
	return nil
}
