//go:build unix

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9): bookmarks.toggle sets a global mark on the row (the
// laptop's bookmarks) or takes it away; bookmarks.list names the marks
// in the workspace with their rows; bookmarks.clear takes them all away.
func TestWebBookmarks(t *testing.T) {
	r, b := webEditRig(t, "one\ntwo\nthree\n")
	var on webproto.BookmarkToggle
	webResData(t, r.call(webproto.OpBookmarksToggle, &webproto.BufLineArgs{Buf: b.Buf, Line: 1}), &on)
	if !on.On {
		t.Fatalf("toggle: %+v", on)
	}
	webResData(t, r.call(webproto.OpBookmarksToggle, &webproto.BufLineArgs{Buf: b.Buf, Line: 2}), &on)
	var list webproto.BookmarkList
	webResData(t, r.call(webproto.OpBookmarksList, &webproto.EmptyArgs{}), &list)
	var got []string
	for _, it := range list.Items {
		got = append(got, it.Mark+" "+it.Path+":"+itoa(it.Line)+" "+it.Text)
	}
	if strings.Join(got, "|") != "A f.txt:1 two|B f.txt:2 three" {
		t.Fatalf("list %q", got)
	}
	webResData(t, r.call(webproto.OpBookmarksToggle, &webproto.BufLineArgs{Buf: b.Buf, Line: 1}), &on)
	if on.On {
		t.Fatal("the second toggle kept the mark")
	}
	webResData(t, r.call(webproto.OpBookmarksList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 1 || list.Items[0].Line != 2 {
		t.Fatalf("after toggle off %+v", list.Items)
	}
	if f := r.call(webproto.OpBookmarksClear, &webproto.EmptyArgs{}); f["t"] != "res" {
		t.Fatal(f)
	}
	webResData(t, r.call(webproto.OpBookmarksList, &webproto.EmptyArgs{}), &list)
	if len(list.Items) != 0 {
		t.Fatalf("after clear %+v", list.Items)
	}
	if f := r.call(webproto.OpBookmarksToggle, &webproto.BufLineArgs{Buf: 999, Line: 0}); webErrCode(f) != "not_found" {
		t.Errorf("unopened buffer: %v", f)
	}
}

// Spec (WEBP-9): files.recent lists the laptop's recent files that are
// in the workspace and still there, newest first, workspace-relative.
func TestWebFilesRecent(t *testing.T) {
	r := newWebFilesRig(t)
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	a := r.write("a.go", "package a\n")
	sub := r.write("pkg/b.go", "package pkg\n")
	now := time.Now()
	entries := []RecentEntry{
		{Path: sub, OpenedAt: now},
		{Path: "/elsewhere/x.go", OpenedAt: now.Add(-time.Minute)},
		{Path: filepath.Join(r.root, "gone.go"), OpenedAt: now.Add(-2 * time.Minute)},
		{Path: a, OpenedAt: now.Add(-3 * time.Minute)},
	}
	data, _ := json.Marshal(entries)
	if err := os.MkdirAll(filepath.Join(cfg, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "termocode", "recents.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var res webproto.FilesRecent
	webResData(t, r.call(webproto.OpFilesRecent, &webproto.EmptyArgs{}), &res)
	if strings.Join(res.Paths, "|") != "pkg/b.go|a.go" {
		t.Fatalf("recent %q", res.Paths)
	}
}

// fakeWebSymbolsLua starts a server on buffer b that answers
// workspace/symbol with two symbols, one in the workspace and one out.
const fakeWebSymbolsLua = `
local b, root, other = ...
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { positionEncoding = 'utf-16', textDocumentSync = 1, workspaceSymbolProvider = true } }
      elseif method == 'workspace/symbol' then
        _G.web_ws_query = params.query
        local function at(f, l, c) return { uri = vim.uri_from_fname(f), range = { start = { line = l, character = c }, ['end'] = { line = l, character = c + 3 } } } end
        res = { { name = 'Handler', kind = 23, containerName = 'pkg', location = at(other, 2, 5) },
          { name = 'HandleFar', kind = 12, location = at('/elsewhere/far.go', 0, 0) } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fake-ws', cmd = server, root_dir = root }, { bufnr = b })
vim.wait(2000, function() return #vim.lsp.get_clients({ bufnr = b }) > 0 end)
return id`

// Spec (WEBP-9): lsp.workspace_symbols asks the servers of a buffer for
// a query; places outside the workspace are left out.
func TestWebWorkspaceSymbols(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("a.go", "package a\n")
	other := r.write("pkg/h.go", "package pkg\n\ntype Handler struct{}\n")
	b := webBufOpen(t, r, r.c, "a.go")
	var id any
	r.lua(fakeWebSymbolsLua, &id, b.Buf, r.root, other)
	if id == nil {
		t.Fatal("the fake server did not start")
	}
	var res webproto.LspSymbols
	webResData(t, r.callLspAny(webproto.OpLspWorkspaceSymbols, &webproto.SymbolQueryArgs{Buf: b.Buf, Query: "Hand"}), &res)
	if len(res.Items) != 1 {
		t.Fatalf("symbols %+v", res)
	}
	if s := res.Items[0]; s.Name != "Handler" || s.Kind != 23 || s.Path != "pkg/h.go" || s.Line != 2 || s.Col != 5 || s.Container != "pkg" {
		t.Errorf("symbol %+v", s)
	}
	var q string
	r.lua(`return _G.web_ws_query`, &q)
	if q != "Hand" {
		t.Errorf("the server got query %q", q)
	}
}

// Spec (WEBP-9): snippets.list gives the snippets of the buffer's file
// type (the laptop's bundled and user snippets), sorted by trigger.
func TestWebSnippetsList(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("a.go", "package a\n")
	b := webBufOpen(t, r, r.c, "a.go")
	r.lua(`_G.termocode_snippets_for = function(ft)
  if ft ~= 'go' then return {} end
  return { iferr = 'if err != nil {\n\treturn ${1:err}\n}', fn = 'func ${1:name}() {\n\t$0\n}' }
end`, nil)
	var res webproto.SnippetList
	webResData(t, r.call(webproto.OpSnippetsList, &webproto.BufArgs{Buf: b.Buf}), &res)
	if len(res.Items) != 2 || res.Items[0].Trigger != "fn" || res.Items[1].Trigger != "iferr" || res.Items[1].Body != "if err != nil {\n\treturn ${1:err}\n}" {
		t.Fatalf("snippets %+v", res.Items)
	}
}

// callLspAny runs an op that waits for nvim's notification (as callLsp
// does) with any args.
func (r *webFilesRig) callLspAny(op string, args any) map[string]any {
	r.t.Helper()
	r.id++
	c := r.c
	c.inflight.Add(1)
	call := &webCall{ctx: c.ctx, conn: c, id: r.id, op: op, args: args}
	cmd, found := r.m.webRunCall(r.h, call)
	if !found {
		r.t.Fatalf("no handler for %s", op)
	}
	done := make(chan tea.Msg, 1)
	go func() {
		if cmd == nil {
			done <- nil
			return
		}
		done <- cmd()
	}()
	r.pumpUntil(op+" answer", call.answered.Load)
	select {
	case msg := <-done:
		if f, ok := msg.(applyMsg); ok {
			f(r.m)
		}
	case <-time.After(20 * time.Second):
		r.t.Fatal("the wait did not end")
	}
	for _, f := range r.takeOn(c) {
		if id, ok := f["id"].(float64); ok && uint64(id) == r.id {
			return f
		}
		r.keep(c, f)
	}
	r.t.Fatalf("%s gave no answer", op)
	return nil
}
