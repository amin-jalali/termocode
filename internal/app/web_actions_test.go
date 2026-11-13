//go:build unix

package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// fakeWebActionsLua starts a server on buffer b with two code actions
// (the first preferred: it adds a row at the top) and inlay hints (a
// plain label and one made of parts).
const fakeWebActionsLua = `
local b, root = ...
local function pos(l, c) return { line = l, character = c } end
local uri = vim.uri_from_bufnr(b)
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { positionEncoding = 'utf-16', textDocumentSync = 1, codeActionProvider = true, inlayHintProvider = true } }
      elseif method == 'textDocument/codeAction' then
        _G.web_ca_range = params.range.start.line .. ':' .. params.range.start.character .. '-' .. params.range['end'].line .. ':' .. params.range['end'].character
        res = {
          { title = 'Add import', kind = 'quickfix', isPreferred = true,
            edit = { changes = { [uri] = { { range = { start = pos(1, 0), ['end'] = pos(1, 0) }, newText = 'import "fmt"\n' } } } } },
          { title = 'Rename x', kind = 'refactor', edit = { changes = { [uri] = { { range = { start = pos(2, 4), ['end'] = pos(2, 5) }, newText = 'y' } } } } },
        }
      elseif method == 'textDocument/inlayHint' then
        res = { { position = pos(2, 5), label = ': int', paddingLeft = false },
          { position = pos(2, 9), label = { { value = 'n' }, { value = ':' } }, paddingRight = true } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fake-ca', cmd = server, root_dir = root }, { bufnr = b })
vim.wait(2000, function() return #vim.lsp.get_clients({ bufnr = b }) > 0 end)
return id`

func webActionsRig(t *testing.T) (*webFilesRig, webproto.BufOpenRes) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("a.go", "package a\n\nvar x = f(1)\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var id any
	r.lua(fakeWebActionsLua, &id, b.Buf, r.root)
	if id == nil {
		t.Fatal("the fake server did not start")
	}
	return r, b
}

// Spec (WEBP-9): lsp.code_actions lists the servers' actions for a range
// (titles, kinds, the preferred one); lsp.code_action_apply applies one
// of them on the laptop at the tick they were listed at; a buffer that
// moved on since is a conflict that changes nothing.
func TestWebCodeActions(t *testing.T) {
	r, b := webActionsRig(t)
	var list webproto.LspActions
	webResData(t, r.callLspAny(webproto.OpLspCodeActions, &webproto.LspRangeArgs{Buf: b.Buf, Tick: b.Tick, Line: 2, Col: 4, EndLine: 2, EndCol: 5}), &list)
	if len(list.Items) != 2 || list.Items[0].Title != "Add import" || !list.Items[0].Preferred || list.Items[0].Kind != "quickfix" || list.Items[1].Title != "Rename x" || list.Items[1].Preferred {
		t.Fatalf("actions %+v", list)
	}
	var rng string
	r.lua(`return _G.web_ca_range`, &rng)
	if rng != "2:4-2:5" {
		t.Errorf("the server got range %s", rng)
	}
	if f := r.call(webproto.OpLspCodeActionApply, &webproto.LspApplyArgs{Buf: b.Buf, Tick: b.Tick + 3, Index: 0}); webErrCode(f) != "conflict" {
		t.Fatalf("stale tick: %v", f)
	}
	var res webproto.EditRes
	webResData(t, r.call(webproto.OpLspCodeActionApply, &webproto.LspApplyArgs{Buf: b.Buf, Tick: b.Tick, Index: 0}), &res)
	if got := webBufLines(r, b.Buf); got != `package a|import "fmt"||var x = f(1)` || !res.Dirty {
		t.Fatalf("applied %q, %+v", got, res)
	}
	// The list belongs to the old tick now.
	if f := r.call(webproto.OpLspCodeActionApply, &webproto.LspApplyArgs{Buf: b.Buf, Tick: res.Tick, Index: 1}); webErrCode(f) != "conflict" {
		t.Errorf("a list of an older tick: %v", f)
	}
}

// Spec (WEBP-9): lsp.inlay_hints gives the hints of rows first..last
// with their labels (parts joined) and padding.
func TestWebInlayHints(t *testing.T) {
	r, b := webActionsRig(t)
	var res webproto.LspHints
	webResData(t, r.callLspAny(webproto.OpLspInlayHints, &webproto.LspRowsArgs{Buf: b.Buf, Tick: b.Tick, First: 0, Last: 2}), &res)
	if len(res.Items) != 2 {
		t.Fatalf("hints %+v", res)
	}
	if h := res.Items[0]; h.Line != 2 || h.Col != 5 || h.Label != ": int" || h.PadLeft || h.PadRight {
		t.Errorf("hint 0 %+v", h)
	}
	if h := res.Items[1]; h.Line != 2 || h.Col != 9 || h.Label != "n:" || !h.PadRight {
		t.Errorf("hint 1 %+v", h)
	}
}
