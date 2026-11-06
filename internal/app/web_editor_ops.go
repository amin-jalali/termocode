package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// More of the desktop editor's commands (WEBP-9, Editor and files):
// Format Document with the laptop's language server, Revert (read the
// file from disk again; the page asks first, Cancel first) and the
// Markdown preview of a buffer. Only a buffer the browser opened: it
// passed the workspace and secret checks.

func init() {
	webOps[webproto.OpBufFormat] = webOpBufFormat
	webOps[webproto.OpBufReload] = webOpBufReload
	webOps[webproto.OpBufMarkdown] = webOpBufMarkdown
}

// webLuaFormat formats buffer b at changedtick tick with the servers
// that can (the same vim.lsp.buf.format the laptop's command runs).
const webLuaFormat = `
local b, tick = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local cur = api.nvim_buf_get_changedtick(b)
if cur ~= tick then return { err = 'conflict', tick = cur } end
if not vim.bo[b].modifiable then return { err = 'readonly' } end
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local can = false
for _, c in ipairs(get({ bufnr = b })) do
  if (c.server_capabilities or {}).documentFormattingProvider then can = true end
end
if not can then return { err = 'none' } end
local ok, e = pcall(vim.lsp.buf.format, { bufnr = b, async = false, timeout_ms = 3000 })
if not ok then return { err = 'fail', msg = tostring(e) } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = vim.bo[b].modified }`

// webLuaReload reads buffer b's file again (:edit!), dropping its
// unsaved changes.
const webLuaReload = `
local b = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local ok, e = pcall(api.nvim_buf_call, b, function() vim.cmd('silent edit!') end)
if not ok then return { err = 'fail', msg = tostring(e) } end
return { tick = api.nvim_buf_get_changedtick(b), dirty = vim.bo[b].modified }`

// webLuaBufText is buffer b's text, at most max bytes (cut: longer).
const webLuaBufText = `
local b, max = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local out, n = {}, 0
for _, l in ipairs(api.nvim_buf_get_lines(b, 0, -1, false)) do
  if n + #l + 1 > max then return { text = table.concat(out, '\n'), cut = true } end
  out[#out + 1] = l
  n = n + #l + 1
end
return { text = table.concat(out, '\n') }`

// webOpBufFormat runs off Update: a server may take its time.
func webOpBufFormat(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.BufTickArgs)
	b, ok := m.webEditBuf(call, a.Buf)
	if !ok {
		return nil
	}
	client, n := m.nvim, b.buf
	root, _ := m.rootDir()
	call.later()
	return func() tea.Msg {
		var res webLuaEdit
		if err := client.ExecLuaResult(webLuaFormat, &res, n, a.Tick); err != nil {
			call.fail(webproto.CodeInternal, webNvimText(root, err.Error(), "The file was not formatted."))
			return nil
		}
		switch res.Err {
		case "":
			call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
		case "conflict":
			webFailData(call, webproto.CodeConflict, "The file changed. Format it again.", webproto.ConflictData{Tick: res.Tick})
		case "none":
			call.fail(webproto.CodeNotFound, "No formatter is available for this file.")
		case "readonly":
			call.fail(webproto.CodeForbidden, "This file is read only.")
		case "gone":
			call.fail(webproto.CodeGone, "This file was closed on the laptop.")
		default:
			call.fail(webproto.CodeInternal, webNvimText(root, res.Msg, "The file was not formatted."))
		}
		return nil
	}
}

func webOpBufReload(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	var res webLuaEdit
	if err := m.nvim.ExecLuaResult(webLuaReload, &res, b.buf); err != nil {
		webFail(call, err)
		return nil
	}
	switch res.Err {
	case "":
		call.reply(webproto.EditRes{Tick: res.Tick, Dirty: res.Dirty})
	case "gone":
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
	default:
		call.fail(webproto.CodeInternal, m.webNvimText(res.Msg, "The file was not read again."))
	}
	return nil
}

type webLuaText struct {
	Err  string `msgpack:"err"`
	Text string `msgpack:"text"`
	Cut  bool   `msgpack:"cut"`
}

// webOpBufMarkdown parses off Update (a long file takes a moment).
func webOpBufMarkdown(m *Model, call *webCall) tea.Cmd {
	b, ok := m.webEditBuf(call, call.args.(*webproto.BufArgs).Buf)
	if !ok {
		return nil
	}
	var res webLuaText
	if err := m.nvim.ExecLuaResult(webLuaBufText, &res, b.buf, webproto.MaxMarkdownSrc); err != nil {
		webFail(call, err)
		return nil
	}
	if res.Err != "" {
		call.fail(webproto.CodeGone, "This file was closed on the laptop.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		call.reply(webproto.BufMarkdown{MD: webMarkdownParse(res.Text), Cut: res.Cut})
		return nil
	}
}
