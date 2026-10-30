package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webSymbols(r *webFilesRig, buf int) webproto.BufSymbols {
	r.t.Helper()
	var res webproto.BufSymbols
	webResData(r.t, r.callOn(r.c, webproto.OpBufSymbols, &webproto.BufArgs{Buf: buf}), &res)
	return res
}

func webSymText(res webproto.BufSymbols) string {
	var out []string
	for _, s := range res.Symbols {
		out = append(out, fmt.Sprintf("%s:%d:%d:%d:%d", s.Name, s.Kind, s.Line, s.Col, s.Depth))
	}
	return res.Source + " " + strings.Join(out, " ")
}

// webFakeSymbolsLSP attaches a language server to buffer b that answers
// documentSymbol with a class holding a method.
const webFakeSymbolsLSP = `
local b = ...
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then res = { capabilities = { textDocumentSync = 1, documentSymbolProvider = true } }
      elseif method == 'textDocument/documentSymbol' then
        local function r(l, c) return { start = { line = l, character = c }, ['end'] = { line = l, character = c + 3 } } end
        res = { { name = 'Shop', kind = 5, range = r(0, 0), selectionRange = r(0, 6), children = {
          { name = 'buy', detail = '(self)', kind = 6, range = r(1, 4), selectionRange = r(1, 8) } } } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
vim.lsp.start({ name = 'syms', cmd = server, root_dir = vim.fn.getcwd() }, { bufnr = b })
vim.wait(2000, function()
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return #get({ bufnr = b }) == 1
end)`

// Spec (explorer OUTLINE): buf.symbols answers the language server's
// document symbols of a buffer the page opened, nested by depth; with no
// server, the hub's guess from declarations (source "syntax"); a file no
// lexer knows has none; a buffer the page did not open is not_found.
func TestBufSymbolsLSPThenSyntax(t *testing.T) {
	r := newWebFilesRig(t)
	nv := r.withNvim()
	src := "class Shop:\n    def buy(self):\n        pass\n"
	r.write("shop.py", src)
	r.write("plain.zzz", "def x\n")
	b := webBufOpen(t, r, r.c, "shop.py")
	if got, want := webSymText(webSymbols(r, b.Buf)), "syntax Shop:5:1:7:0 buy:6:2:9:1"; got != want {
		t.Errorf("no server: %s, want %s", got, want)
	}
	var ok any
	if err := nv.ExecLuaResult(webFakeSymbolsLSP, &ok, b.Buf); err != nil {
		t.Fatal(err)
	}
	if got, want := webSymText(webSymbols(r, b.Buf)), "lsp Shop:5:1:7:0 buy:6:2:9:1"; got != want {
		t.Errorf("with a server: %s, want %s", got, want)
	}
	if res := webSymbols(r, b.Buf); res.Symbols[1].Detail != "(self)" {
		t.Errorf("detail %+v", res.Symbols[1])
	}
	p := webBufOpen(t, r, r.c, "plain.zzz")
	if got := webSymText(webSymbols(r, p.Buf)); got != "none " {
		t.Errorf("unknown file: %q", got)
	}
	if got := webTestErr(r.callOn(r.c, webproto.OpBufSymbols, &webproto.BufArgs{Buf: 9999})); got != "not_found" {
		t.Errorf("a buffer the page did not open: %q", got)
	}
}
