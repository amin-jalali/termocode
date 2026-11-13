//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// fakeWebNavLua starts an in-process language server on buffer b that
// knows type definitions, implementations and formatting: a type
// definition and an implementation in other.go, and a formatting edit
// that puts "// formatted" over the first row.
const fakeWebNavLua = `
local b, root, other = ...
local function pos(l, c) return { line = l, character = c } end
local function loc(f, l, c0, c1) return { uri = vim.uri_from_fname(f), range = { start = pos(l, c0), ['end'] = pos(l, c1) } } end
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { positionEncoding = 'utf-16', textDocumentSync = 1,
          typeDefinitionProvider = true, implementationProvider = true, documentFormattingProvider = true } }
      elseif method == 'textDocument/typeDefinition' then
        res = { loc(other, 2, 5, 8) }
      elseif method == 'textDocument/implementation' then
        res = { loc(other, 4, 0, 4) }
      elseif method == 'textDocument/formatting' then
        res = { { range = { start = pos(0, 0), ['end'] = pos(0, 0) }, newText = '// formatted\n' } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fake-nav', cmd = server, root_dir = root }, { bufnr = b })
vim.wait(2000, function() return #vim.lsp.get_clients({ bufnr = b }) > 0 end)
return id`

func webNavRig(t *testing.T) (*webFilesRig, webproto.BufOpenRes) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("a.go", "package a\nvar x T\n")
	other := r.write("other.go", "package a\n\ntype T int\n\nfunc (T) M() {}\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var id any
	r.lua(fakeWebNavLua, &id, b.Buf, r.root, other)
	if id == nil {
		t.Fatal("the fake server did not start")
	}
	return r, b
}

// Spec (WEBP-9): lsp.type_definition and lsp.implementation ask the
// laptop's language server like lsp.definition and answer workspace
// places.
func TestLspTypeDefinitionAndImplementation(t *testing.T) {
	r, b := webNavRig(t)
	var locs webproto.LspLocations
	webResData(t, r.callLsp(webproto.OpLspTypeDefinition, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: 6}), &locs)
	if len(locs.Locs) != 1 || locs.Locs[0].Path != "other.go" || locs.Locs[0].Line != 2 || locs.Locs[0].Col != 5 || locs.Locs[0].Text != "type T int" {
		t.Fatalf("type definition %+v", locs)
	}
	webResData(t, r.callLsp(webproto.OpLspImplementation, &webproto.LspPosArgs{Buf: b.Buf, Tick: b.Tick, Line: 1, Col: 6}), &locs)
	if len(locs.Locs) != 1 || locs.Locs[0].Path != "other.go" || locs.Locs[0].Line != 4 || locs.Locs[0].Text != "func (T) M() {}" {
		t.Fatalf("implementation %+v", locs)
	}
}

// Spec (WEBP-9): buf.format formats the buffer with the laptop's
// language server at the page's tick (an older tick is a conflict and
// changes nothing); a buffer no server formats says so.
func TestBufFormat(t *testing.T) {
	r, b := webNavRig(t)
	if f := r.call(webproto.OpBufFormat, &webproto.BufTickArgs{Buf: b.Buf, Tick: b.Tick + 7}); webErrCode(f) != "conflict" || webBufLines(r, b.Buf) != "package a|var x T" {
		t.Fatalf("stale tick: %v, %q", f, webBufLines(r, b.Buf))
	}
	var res webproto.EditRes
	webResData(t, r.call(webproto.OpBufFormat, &webproto.BufTickArgs{Buf: b.Buf, Tick: b.Tick}), &res)
	if got := webBufLines(r, b.Buf); got != "// formatted|package a|var x T" || !res.Dirty || res.Tick <= b.Tick {
		t.Fatalf("formatted %q, %+v", got, res)
	}
	r.write("n.txt", "plain\n")
	n := webBufOpen(t, r, r.c, "n.txt")
	if f := r.call(webproto.OpBufFormat, &webproto.BufTickArgs{Buf: n.Buf, Tick: n.Tick}); webErrCode(f) != "not_found" {
		t.Errorf("no formatter: %v", f)
	}
}

// Spec (WEBP-9): buf.reload reads the file from disk again and drops the
// buffer's unsaved changes; buf.markdown gives the buffer's text as a
// Markdown tree.
func TestBufReloadAndMarkdown(t *testing.T) {
	r, b := webEditRig(t, "# Title\n\nSome **bold** text.\n")
	webSetText(r, r.c, b.Buf, b.Tick, 0, 0, 0, 0, "x")
	var md webproto.BufMarkdown
	webResData(t, r.call(webproto.OpBufMarkdown, &webproto.BufArgs{Buf: b.Buf}), &md)
	if md.MD == nil || len(md.MD.C) < 2 || md.MD.C[0].K != webproto.MdParagraph || md.MD.C[1].K != webproto.MdParagraph {
		// "x# Title" is no heading any more.
		t.Fatalf("markdown of the edited buffer %+v", md.MD)
	}
	if err := os.WriteFile(filepath.Join(r.root, "f.txt"), []byte("# New\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var res webproto.EditRes
	webResData(t, r.call(webproto.OpBufReload, &webproto.BufArgs{Buf: b.Buf}), &res)
	if got := webBufLines(r, b.Buf); got != "# New" || res.Dirty {
		t.Fatalf("reloaded %q, %+v", got, res)
	}
	webResData(t, r.call(webproto.OpBufMarkdown, &webproto.BufArgs{Buf: b.Buf}), &md)
	if md.MD == nil || len(md.MD.C) != 1 || md.MD.C[0].K != webproto.MdHeading || md.MD.C[0].Level != 1 {
		t.Fatalf("markdown %+v", md.MD)
	}
	if f := r.call(webproto.OpBufReload, &webproto.BufArgs{Buf: 999}); webErrCode(f) != "not_found" {
		t.Errorf("unopened buffer: %v", f)
	}
}
