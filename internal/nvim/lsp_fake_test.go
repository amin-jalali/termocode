package nvim

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolatedClient starts a real nvim that never sees the user's own
// config, data or state.
func isolatedClient(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	base := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(base, strings.ToLower(v)))
	}
	c, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return c
}

// fakeLSPLua starts an in-process language server (vim.lsp.start with a
// Lua cmd) on the current buffer. It answers each method with the result
// from the JSON table passed in, and records the methods it saw.
const fakeLSPLua = `
-- Decode like nvim's own LSP transport: JSON null becomes nil.
local results = vim.json.decode(..., { luanil = { object = true, array = true } })
local function server(dispatchers)
  local closing, n = false, 0
  return {
    request = function(method, params, callback)
      n = n + 1
      local res
      if method == 'initialize' then
        res = { capabilities = {
          documentSymbolProvider = true, workspaceSymbolProvider = true,
          referencesProvider = true, hoverProvider = true,
          codeActionProvider = { resolveProvider = true },
          typeDefinitionProvider = true, implementationProvider = true,
          executeCommandProvider = { commands = { 'fake.cmd' } },
        } }
      elseif method == 'shutdown' then
        res = nil
      else
        _G._fake_seen = _G._fake_seen or {}
        table.insert(_G._fake_seen, method)
        res = results[method]
      end
      vim.schedule(function() callback(nil, res) end)
      return true, n
    end,
    notify = function(method)
      if method == 'exit' then closing = true end
      return true
    end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fakels', cmd = server, root_dir = vim.fn.getcwd() }, { bufnr = 0 })
local ok = vim.wait(5000, function()
  local c = vim.lsp.get_client_by_id(id)
  return c ~= nil and c.initialized
end)
return ok and 'ready' or 'not ready'
`

func startFakeLSP(t *testing.T, c *Client, results map[string]any) {
	t.Helper()
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.EvalLuaStringArgs(fakeLSPLua, string(b))
	if err != nil || got != "ready" {
		t.Fatalf("fake LSP: %q %v", got, err)
	}
}

func seenMethods(t *testing.T, c *Client) string {
	t.Helper()
	s, err := c.EvalLuaString(`return table.concat(_G._fake_seen or {}, ',')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func editTemp(t *testing.T, c *Client, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenFile("edit", p); err != nil {
		t.Fatal(err)
	}
	return p
}

func lspRange(line, char int) map[string]any {
	pos := map[string]any{"line": line, "character": char}
	return map[string]any{"start": pos, "end": pos}
}

func TestLSPQueriesWithoutAServer(t *testing.T) {
	c := isolatedClient(t)
	editTemp(t, c, "a.go", "package a\n")
	if _, err := c.DocumentSymbols(); !IsNoLSP(err) {
		t.Fatalf("DocumentSymbols err = %v, want no LSP", err)
	}
	if ws, err := c.WorkspaceSymbols("x"); err != nil || len(ws) != 0 {
		t.Fatalf("WorkspaceSymbols = %v, %v", ws, err)
	}
	if refs, err := c.References(); err != nil || len(refs) != 0 {
		t.Fatalf("References = %v, %v", refs, err)
	}
	if acts, err := c.FetchCodeActions(); err != nil || len(acts) != 0 {
		t.Fatalf("FetchCodeActions = %v, %v", acts, err)
	}
	if h, err := c.Hover(); err != nil || h != "" {
		t.Fatalf("Hover = %q, %v", h, err)
	}
	if ok, err := c.GotoTypeDefinition(); err != nil || ok {
		t.Fatalf("GotoTypeDefinition = %v, %v", ok, err)
	}
	// An index nobody fetched does nothing.
	if err := c.ApplyCodeAction(3); err != nil {
		t.Fatal(err)
	}
}

func TestLSPQueriesThroughAFakeServer(t *testing.T) {
	c := isolatedClient(t)
	src := editTemp(t, c, "main.go", "package main\n\nfunc main() {}\n")
	other := filepath.Join(filepath.Dir(src), "other.go")
	if err := os.WriteFile(other, []byte("package main\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file://" + other
	startFakeLSP(t, c, map[string]any{
		"textDocument/documentSymbol": []any{
			map[string]any{"name": "main", "detail": "func()", "kind": 12, "range": lspRange(2, 0), "selectionRange": lspRange(2, 5),
				"children": []any{map[string]any{"name": "inner", "kind": 13, "range": lspRange(2, 9)}, map[string]any{"name": ""}}},
			map[string]any{"name": "flat", "kind": 13, "location": map[string]any{"uri": uri, "range": lspRange(1, 4)}},
			"not a symbol",
		},
		"workspace/symbol": []any{
			map[string]any{"name": "x", "containerName": "main", "kind": 13, "location": map[string]any{"uri": uri, "range": lspRange(1, 4)}},
		},
		"textDocument/references": []any{
			map[string]any{"uri": uri, "range": lspRange(1, 4)},
		},
		"textDocument/hover": map[string]any{"contents": map[string]any{"kind": "markdown", "value": "**x** is an int"}},
		"textDocument/codeAction": []any{
			map[string]any{"title": "Add import", "kind": "quickfix", "isPreferred": true,
				"edit": map[string]any{"changes": map[string]any{"file://" + src: []any{
					map[string]any{"range": lspRange(0, 0), "newText": "// fixed\n"}}}}},
			map[string]any{"title": "Run thing", "kind": "source", "command": map[string]any{"title": "t", "command": "fake.cmd"}},
		},
		"textDocument/typeDefinition": []any{map[string]any{"uri": uri, "range": lspRange(1, 4)}},
		"textDocument/implementation": nil,
	})

	syms, err := c.DocumentSymbols()
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 2 || syms[0].Name != "main" || syms[0].Line != 3 || syms[0].Col != 6 || syms[0].Detail != "func()" ||
		len(syms[0].Children) != 1 || syms[0].Children[0].Line != 3 || syms[1].Name != "flat" || syms[1].Line != 2 || syms[1].Col != 5 {
		t.Fatalf("symbols %+v", syms)
	}

	ws, err := c.WorkspaceSymbols("x")
	if err != nil || len(ws) != 1 || ws[0].Name != "x" || ws[0].Container != "main" || ws[0].File != other || ws[0].Line != 2 || ws[0].Col != 5 {
		t.Fatalf("workspace symbols %+v %v", ws, err)
	}

	refs, err := c.References()
	if err != nil || len(refs) != 1 || refs[0].File != other || refs[0].Line != 2 || refs[0].Preview != "var x = 1" {
		t.Fatalf("references %+v %v", refs, err)
	}

	if h, err := c.Hover(); err != nil || h != "**x** is an int" {
		t.Fatalf("hover %q %v", h, err)
	}

	acts, err := c.FetchCodeActions()
	if err != nil || len(acts) != 2 || acts[0].Title != "Add import" || !acts[0].IsPreferred || acts[0].Client != "fakels" || acts[1].Kind != "source" {
		t.Fatalf("code actions %+v %v", acts, err)
	}
	if err := c.ApplyCodeAction(acts[0].Index); err != nil {
		t.Fatal(err)
	}
	if text, _ := c.BufferContent(); !strings.HasPrefix(text, "// fixed\npackage main") {
		t.Fatalf("edit not applied: %q", text)
	}
	if err := c.ApplyCodeAction(acts[1].Index); err != nil {
		t.Fatal(err)
	}

	if ok, err := c.GotoImplementation(); err != nil || ok {
		t.Fatalf("implementation with no answer = %v %v", ok, err)
	}
	if ok, err := c.GotoTypeDefinition(); err != nil || !ok {
		t.Fatalf("type definition = %v %v", ok, err)
	}
	if name, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`); name != other {
		t.Fatalf("jumped to %q, want %q", name, other)
	}
	if seen := seenMethods(t, c); !strings.Contains(seen, "workspace/executeCommand") {
		t.Fatalf("command not run; server saw %s", seen)
	}
}

func TestAllDiagnosticsAcrossBuffers(t *testing.T) {
	c := isolatedClient(t)
	a := editTemp(t, c, "a.go", "one\ntwo\n")
	if err := c.ExecLua(`
		local ns = vim.api.nvim_create_namespace('test')
		vim.diagnostic.set(ns, 0, {
			{ lnum = 1, col = 2, severity = vim.diagnostic.severity.ERROR, source = 'vet', message = 'bad two' },
			{ lnum = 0, col = 0, severity = vim.diagnostic.severity.HINT, message = 'hint one' },
		})`); err != nil {
		t.Fatal(err)
	}
	got, err := c.AllDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("diagnostics %+v", got)
	}
	byMsg := map[string]Diagnostic{}
	for _, d := range got {
		byMsg[d.Message] = d
	}
	if d := byMsg["bad two"]; d.Path != a || d.Line != 2 || d.Col != 3 || d.Severity != 1 || d.Source != "vet" {
		t.Fatalf("error diag %+v", d)
	}
	if d := byMsg["hint one"]; d.Severity != 4 || d.Source != "" {
		t.Fatalf("hint diag %+v", d)
	}
}

func TestParseSymbolsOddShapes(t *testing.T) {
	got := parseSymbols([]interface{}{
		map[string]interface{}{"name": "noRange", "kind": int64(5)},
		map[string]interface{}{"name": "badStart", "range": map[string]interface{}{}},
		map[string]interface{}{"name": "negative", "selectionRange": map[string]interface{}{"start": map[string]interface{}{"line": int64(-5), "character": int64(-2)}}},
		42,
	})
	if len(got) != 3 || got[0].Line != 0 || got[1].Line != 1 || got[1].Col != 1 || got[2].Line != 1 || got[2].Col != 1 {
		t.Fatalf("got %+v", got)
	}
}
