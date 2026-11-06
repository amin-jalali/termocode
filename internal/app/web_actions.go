package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webLuaApplyAction applies action idx of the list lsp.code_actions gave
// for buffer b at tick (the laptop's own vim.lsp: a workspace edit, then
// the action's command), resolving it first when the server wants that.
const webLuaApplyAction = `
local b, tick, idx = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local cur = api.nvim_buf_get_changedtick(b)
local ca = _G.termocode_web_ca
if cur ~= tick or not ca or ca.buf ~= b or ca.tick ~= tick then return { err = 'conflict', tick = cur } end
local it = ca.list[idx + 1]
if not it then return { err = 'none' } end
local c = vim.lsp.get_client_by_id(it.cid)
if not c then return { err = 'none' } end
local a = it.a
local caps = c.server_capabilities or {}
if not a.edit and type(a.command) ~= 'string' and type(caps.codeActionProvider) == 'table' and caps.codeActionProvider.resolveProvider then
  local ok, r
  if vim.fn.has('nvim-0.11') == 1 then ok, r = pcall(c.request_sync, c, 'codeAction/resolve', a, 3000, b)
  else ok, r = pcall(c.request_sync, 'codeAction/resolve', a, 3000, b) end
  if ok and r and r.result then a = r.result end
end
_G.termocode_web_ca = nil
local ok, e = pcall(function()
  if a.edit then vim.lsp.util.apply_workspace_edit(a.edit, c.offset_encoding or 'utf-16') end
  local cmd = type(a.command) == 'table' and a.command or (type(a.command) == 'string' and a or nil)
  if cmd then
    if c.exec_cmd then c:exec_cmd(cmd, { bufnr = b })
    else c.request('workspace/executeCommand', cmd, function() end, b) end
  end
end)
if not ok then return { err = 'fail', msg = tostring(e) } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = vim.bo[b].modified }`

// webOpLspApply applies one of the listed code actions on the laptop.
func (m *Model) webOpLspApply(call *webCall) tea.Cmd {
	a := call.args.(*webproto.LspApplyArgs)
	b, ok := m.webEditBuf(call, a.Buf)
	if !ok {
		return nil
	}
	var res webLuaEdit
	if err := m.nvim.ExecLuaResult(webLuaApplyAction, &res, b.buf, a.Tick, a.Index); err != nil {
		webFail(call, err)
		return nil
	}
	switch res.Err {
	case "":
		call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
	case "conflict":
		webFailData(call, webproto.CodeConflict, "The file changed. Ask for the quick fixes again.", webproto.ConflictData{Tick: res.Tick})
	case "none":
		call.fail(webproto.CodeNotFound, "That quick fix is not there any more.")
	case "gone":
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
	default:
		call.fail(webproto.CodeInternal, m.webNvimText(res.Msg, "The quick fix did not work."))
	}
	return nil
}
