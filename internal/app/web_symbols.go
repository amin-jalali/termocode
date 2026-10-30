package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webhl"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The explorer's OUTLINE on the desktop page (buf.symbols): the symbols
// of a file the page opened. The laptop's language server answers first
// (textDocument/documentSymbol, both answer shapes, nested by depth);
// with no server for the file, the hub guesses them from declarations
// off its own mirror of the buffer (internal/webhl).

func init() {
	webOps[webproto.OpBufSymbols] = webOpBufSymbols
}

// webLuaSymbols answers { none = true } when no server of buffer b
// gives document symbols, else { syms = { {name, detail, kind, line,
// col, depth}, … }, cut }: 1-based line and column, in file order.
const webLuaSymbols = `
local b, max = ...
local api = vim.api
if not api.nvim_buf_is_valid(b) or not api.nvim_buf_is_loaded(b) then return { err = 'gone' } end
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local client
for _, c in ipairs(get({ bufnr = b })) do
  local caps = c.server_capabilities or {}
  if caps.documentSymbolProvider then client = c break end
end
if not client then return { none = true } end
local params = { textDocument = { uri = vim.uri_from_bufnr(b) } }
local ok, r = pcall(client.request_sync, client, 'textDocument/documentSymbol', params, 1500, b)
if not ok then ok, r = pcall(client.request_sync, 'textDocument/documentSymbol', params, 1500, b) end
if not ok or not r or r.err or type(r.result) ~= 'table' then return { none = true } end
local out = { syms = {} }
local function walk(list, depth)
  for _, s in ipairs(list) do
    if #out.syms >= max then out.cut = true return end
    local rg = s.selectionRange or s.range or (s.location and s.location.range)
    if type(s.name) == 'string' and rg and rg.start then
      out.syms[#out.syms + 1] = { name = s.name, detail = s.detail or '', kind = s.kind or 0,
        line = rg.start.line + 1, col = rg.start.character + 1, depth = depth }
    end
    if type(s.children) == 'table' then walk(s.children, depth + 1) end
  end
end
walk(r.result, 0)
return out`

// webLuaSymbolsRes is what webLuaSymbols returns.
type webLuaSymbolsRes struct {
	Err  string `msgpack:"err"`
	None bool   `msgpack:"none"`
	Cut  bool   `msgpack:"cut"`
	Syms []struct {
		Name   string `msgpack:"name"`
		Detail string `msgpack:"detail"`
		Kind   int    `msgpack:"kind"`
		Line   int    `msgpack:"line"`
		Col    int    `msgpack:"col"`
		Depth  int    `msgpack:"depth"`
	} `msgpack:"syms"`
}

// webSymbolDepthMax caps how deep a symbol may nest on the wire.
const webSymbolDepthMax = 16

func webOpBufSymbols(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.BufArgs)
	if m.web == nil || m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	b := m.web.filesState().bufs[a.Buf]
	if b == nil || b.term {
		call.fail(webproto.CodeNotFound, "Open the file first.")
		return nil
	}
	client, n := m.nvim, b.buf
	path, ft, rows := b.path, b.ft, b.rows
	call.later()
	return func() tea.Msg {
		var res webLuaSymbolsRes
		if err := client.ExecLuaResult(webLuaSymbols, &res, n, webproto.MaxSymbols); err != nil {
			recordError("[web] buf.symbols: " + err.Error())
			call.fail(webproto.CodeInternal, "The outline could not be read.")
			return nil
		}
		if res.Err != "" {
			call.fail(webproto.CodeGone, "This file was closed on the laptop.")
			return nil
		}
		out := webproto.BufSymbols{Buf: n, Source: "none", Symbols: []webproto.BufSymbol{}}
		if !res.None {
			out.Source, out.Cut = "lsp", res.Cut
			for _, s := range res.Syms {
				if s.Name == "" || s.Line < 1 || s.Col < 1 {
					continue
				}
				out.Symbols = append(out.Symbols, webproto.BufSymbol{Name: webName(s.Name), Detail: webName(s.Detail), Kind: s.Kind,
					Line: s.Line, Col: s.Col, Depth: min(max(s.Depth, 0), webSymbolDepthMax)})
			}
		} else if syms, cut, ok := webhl.Symbols(path, ft, rows, webproto.MaxSymbols); ok {
			out.Source, out.Symbols, out.Cut = "syntax", syms, cut
		}
		call.reply(out)
		return nil
	}
}
