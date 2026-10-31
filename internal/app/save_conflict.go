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
type saveContinuation uint8

const (
	saveOnly saveContinuation = iota
	saveEveryFile
	saveAndClose
	saveAndQuit
)

const saveConflictLua = `
local target, all = ...
local bufs = all and vim.api.nvim_list_bufs() or {target == 0 and vim.api.nvim_get_current_buf() or target}
for _, b in ipairs(bufs) do
 if vim.api.nvim_buf_is_loaded(b) and (not all or vim.bo[b].buflisted) then
  local name = vim.api.nvim_buf_get_name(b)
  if vim.bo[b].buftype == '' and name ~= '' and _G._termocode_mtime then
   local was, now = vim.b[b].termocode_mtime, _G._termocode_mtime(b)
   if was ~= nil and now ~= nil and was ~= now then
    if vim.bo[b].modified then return b .. '\t' .. vim.fn.fnamemodify(name, ':t') end
    vim.api.nvim_buf_call(b, function() pcall(vim.cmd, 'silent edit') end)
   end
  end
 end
end
return ''`

// askSaveConflict checks before any write and remembers what to resume.
func (m *Model) askSaveConflict(target int, after saveContinuation) bool {
	if m.nvim == nil {
		return false
	}
	out, err := m.nvim.EvalLuaStringArgs(saveConflictLua, target, after == saveEveryFile || after == saveAndQuit)
	if err != nil || out == "" {
		return false
	}
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
	m.saveAfter = after
	return true
}

// saveActiveFile is Ctrl+S: save the current file, or ask first when it
// changed on disk.
func (m *Model) saveActiveFile() tea.Cmd {
	if m.askSaveConflict(0, saveOnly) {
		return nil
	}
	var toastCmd tea.Cmd
	if err := m.editor.Save(); err != nil {
		text := saveFailText(m.editor.Path(), err)
		m.err = text
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Save failed", text)
	} else if p := m.editor.Path(); p != "" {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Saved", saveDetail(p))
		m.onAIProfilesSaved(p)
		toastCmd = tea.Batch(toastCmd, m.onMCPServeConfigSaved(p), m.onWebConfigSaved(p), m.onAIPlanSaved(p)) // mcp_server, web_panel changed; G-PLAN
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// handleSaveConflictConfirm answers the card above.
func (m *Model) handleSaveConflictConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindSaveConflict {
		return nil, false
	}
	after := m.saveAfter
	m.saveAfter = saveOnly
	if id != "overwrite" || m.nvim == nil {
		if after == saveAndClose {
			m.closeQueue = nil
		}
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
	if err == nil && path != "" {
		switch after {
		case saveEveryFile:
			return m.saveAll(), true
		case saveAndQuit:
			return m.onQuitConfirm("save"), true
		case saveAndClose:
			m.confirmKind = confirmKindCloseBuffer
			return m.handleConfirmSelect("save"), true
		}
	}
	return tea.Batch(fetchGitCmd(), c), true
}
