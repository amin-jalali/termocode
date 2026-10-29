package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Save when the file changed on disk (SCN-10). Another program, or a
// second termocode window on the same folder, saved the file after the
// editor read it, and the editor has unsaved edits. A plain :w would ask
// nvim's yes/no question (answered "no" by the prompt guard, so nothing
// was written but the app said "Saved"). Ctrl+S asks instead: Cancel
// (first, focused) keeps both, Overwrite writes the editor's text.

const confirmKindSaveConflict confirmKindEnum = 3410

// saveConflictLua checks the current buffer against its file: the
// mtime fileReloadLua noted when the buffer last matched the file, and
// the mtime on disk now. A modified buffer whose file changed is a
// conflict: it returns "<buf>\t<file name>". An unmodified one is
// reloaded (nothing of the user's is lost), and it returns "".
const saveConflictLua = `
local b = vim.api.nvim_get_current_buf()
local name = vim.api.nvim_buf_get_name(b)
if vim.bo[b].buftype ~= '' or name == '' or not _G._termocode_mtime then return '' end
local was, now = vim.b[b].termocode_mtime, _G._termocode_mtime(b)
if was == nil or now == nil or was == now then return '' end
if not vim.bo[b].modified then
	pcall(vim.cmd, 'silent edit')
	return ''
end
return b .. '\t' .. vim.fn.fnamemodify(name, ':t')`

// saveActiveFile is Ctrl+S: save the current file, or ask first when it
// changed on disk.
func (m *Model) saveActiveFile() tea.Cmd {
	if m.nvim != nil {
		if out, err := m.nvim.EvalLuaString(saveConflictLua); err == nil && out != "" {
			buf, name, _ := strings.Cut(out, "\t")
			id, _ := strconv.Atoi(buf)
			m.confirm = confirm.New("File changed on disk",
				name+" was changed by another program after you opened it.\n\n"+
					"Overwrite it with your text? Cancel keeps both: your edits stay here, the file stays as it is.",
				[]confirm.Button{
					{ID: "cancel", Title: "Cancel"},
					{ID: "overwrite", Title: "Overwrite", Style: confirm.StyleDestructive},
				})
			m.confirm.SetSize(m.w, m.h)
			m.confirmOpen = true
			m.confirmKind = confirmKindSaveConflict
			m.confirmTargetID = id
			return nil
		}
	}
	var toastCmd tea.Cmd
	if err := m.editor.Save(); err != nil {
		text := saveFailText(m.editor.Path(), err)
		m.err = text
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Save failed", text)
	} else if p := m.editor.Path(); p != "" {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Saved", saveDetail(p))
		m.onAIProfilesSaved(p)
		toastCmd = tea.Batch(toastCmd, m.onMCPServeConfigSaved(p), m.onAIPlanSaved(p)) // mcp_server changed; G-PLAN
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// handleSaveConflictConfirm answers the card above.
func (m *Model) handleSaveConflictConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindSaveConflict {
		return nil, false
	}
	if id != "overwrite" || m.nvim == nil {
		return nil, true
	}
	var c tea.Cmd
	path, err := m.nvim.EvalLuaStringArgs(`local b = ...
if not vim.api.nvim_buf_is_valid(b) then return '' end
vim.api.nvim_buf_call(b, function() vim.cmd('silent write!') end)
return vim.api.nvim_buf_get_name(b)`, m.confirmTargetID)
	switch {
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, "Save failed", err.Error())
	case path != "":
		m.toast, c = m.toast.PushDetail(toast.Info, "Saved", saveDetail(path))
	}
	return tea.Batch(fetchGitCmd(), c), true
}
