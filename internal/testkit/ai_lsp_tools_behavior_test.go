package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// K5 rows of TestAIToolsTable: the LSP tools (hover, symbols, nav,
// rename, code actions) through the real app with a fake language
// server attached to a.txt.
func init() {
	aiToolCases = append(aiToolCases, aiLSPToolCases...)
}

// fakeLSPLua attaches an in-process language server to the current
// buffer (a.txt). Its answers are fixed:
//   - hover: "**alpha** is the first word"
//   - documentSymbol: alpha (Function) with child inner (Variable)
//   - workspace/symbol: bravoThing in b.txt line 2
//   - typeDefinition: b.txt line 3; implementation: b.txt line 1 and c.txt line 2
//   - rename: first word of a.txt and of b.txt become newName
//   - codeAction: "Uppercase first line" (a.txt) and "Add header to b.txt"
const fakeLSPLua = `
local root = ...
local ua = vim.uri_from_fname(root .. '/a.txt')
local ub = vim.uri_from_fname(root .. '/b.txt')
local uc = vim.uri_from_fname(root .. '/c.txt')
local function rg(l, c1, c2) return { start = { line = l, character = c1 }, ['end'] = { line = l, character = c2 } } end
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = {
          textDocumentSync = 1, hoverProvider = true, documentSymbolProvider = true,
          workspaceSymbolProvider = true, typeDefinitionProvider = true,
          implementationProvider = true, renameProvider = true, codeActionProvider = true,
        } }
      elseif method == 'textDocument/hover' then
        res = { contents = { kind = 'markdown', value = '**alpha** is the first word' } }
      elseif method == 'textDocument/documentSymbol' then
        res = { { name = 'alpha', kind = 12, range = rg(0, 0, 5), selectionRange = rg(0, 0, 5),
          children = { { name = 'inner', kind = 13, range = rg(1, 0, 5), selectionRange = rg(1, 0, 5) } } } }
      elseif method == 'workspace/symbol' then
        res = { { name = 'bravoThing', kind = 5, location = { uri = ub, range = rg(1, 0, 3) } } }
      elseif method == 'textDocument/typeDefinition' then
        res = { uri = ub, range = rg(2, 0, 5) }
      elseif method == 'textDocument/implementation' then
        res = { { uri = ub, range = rg(0, 0, 3) }, { uri = uc, range = rg(1, 0, 5) } }
      elseif method == 'textDocument/rename' then
        res = { changes = {
          [ua] = { { range = rg(0, 0, 5), newText = params.newName } },
          [ub] = { { range = rg(0, 0, 3), newText = params.newName } },
        } }
      elseif method == 'textDocument/codeAction' then
        res = {
          { title = 'Uppercase first line', kind = 'refactor',
            edit = { changes = { [ua] = { { range = rg(0, 0, 5), newText = 'ALPHA' } } } } },
          { title = 'Add header to b.txt', kind = 'source',
            edit = { changes = { [ub] = { { range = rg(0, 0, 0), newText = '# header\n' } } } } },
        }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
vim.lsp.start({ name = 'fakelsp', cmd = server, root_dir = root })
local ok = vim.wait(2000, function()
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return #get({ bufnr = 0 }) > 0
end)
return tostring(ok)
`

// withFakeLSP is a setup that attaches the fake server to a.txt.
func withFakeLSP(d *Driver) {
	d.T.Helper()
	if got := lua(d, fakeLSPLua, d.Dir); got != "true" {
		d.T.Fatalf("fake LSP did not attach: %q", got)
	}
}

// oneResult is the only tool result, failing the test otherwise.
func oneResult(t *testing.T, r []string) string {
	t.Helper()
	if len(r) != 1 {
		t.Fatalf("results %q", r)
	}
	return r[0]
}

var aiLSPToolCases = []aiToolCase{
	{
		name:  "lsp_hover returns the hover text",
		call:  toolCall{"lsp_hover", map[string]any{"path": "a.txt", "line": 1, "character": 1}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "**alpha** is the first word") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name:  "lsp_symbols for a file lists its symbols with kind and line",
		call:  toolCall{"lsp_symbols", map[string]any{"path": "a.txt"}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "alpha (Function) line 1") || !strings.Contains(got, "  inner (Variable) line 2") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name:  "lsp_symbols with a query searches the workspace",
		call:  toolCall{"lsp_symbols", map[string]any{"query": "bravo"}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "bravoThing (Class) b.txt:2") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name:  "lsp_nav type_definition finds the type",
		call:  toolCall{"lsp_nav", map[string]any{"path": "a.txt", "line": 1, "character": 1, "kind": "type_definition"}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "b.txt:3:1") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name:  "lsp_nav implementation lists every implementation",
		call:  toolCall{"lsp_nav", map[string]any{"path": "a.txt", "line": 1, "character": 1, "kind": "implementation"}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "b.txt:1:1") || !strings.Contains(got, "c.txt:2:1") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name:     "lsp_rename asks, changes two files on screen and disk, no new tab",
		call:     toolCall{"lsp_rename", map[string]any{"path": "a.txt", "line": 1, "character": 1, "new_name": "omega"}},
		setup:    withFakeLSP,
		confirm:  true,
		noReview: true,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "2 files") || !strings.Contains(got, "a.txt") || !strings.Contains(got, "b.txt") {
				t.Errorf("result %q", got)
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "omega") }) {
				t.Errorf("renamed text not on screen:\n%s", screen(d))
			}
			if diskFile(d, "a.txt") != "omega\nbravo\ncharlie\n" || diskFile(d, "b.txt") != "omega\ntwo\nthree\n" {
				t.Errorf("disk a=%q b=%q", diskFile(d, "a.txt"), diskFile(d, "b.txt"))
			}
			if got, _ := tabs(d); len(got) != 1 {
				t.Errorf("tabs %v: rename opened a tab for b.txt", got)
			}
			// The rename is one undo step in the open tab.
			if u := lua(d, `vim.cmd('silent! undo'); return vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]`); u != "alpha" {
				t.Errorf("after undo first line %q", u)
			}
		},
	},
	{
		name: "lsp_rename keeps unsaved user edits unsaved",
		call: toolCall{"lsp_rename", map[string]any{"path": "a.txt", "line": 1, "character": 1, "new_name": "omega"}},
		setup: func(d *Driver) {
			withFakeLSP(d)
			lua(d, `vim.api.nvim_buf_set_lines(0, 2, 3, false, { "mine" }); return ''`)
		},
		confirm:  true,
		noReview: true,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "not saved") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "a.txt") != "alpha\nbravo\ncharlie\n" {
				t.Errorf("a.txt saved with the user's edits: %q", diskFile(d, "a.txt"))
			}
			if bufLines(d) != "omega\nbravo\nmine" {
				t.Errorf("buffer %q", bufLines(d))
			}
			if diskFile(d, "b.txt") != "omega\ntwo\nthree\n" {
				t.Errorf("b.txt %q", diskFile(d, "b.txt"))
			}
		},
	},
	{
		name:  "lsp_code_actions list runs without asking",
		call:  toolCall{"lsp_code_actions", map[string]any{"path": "a.txt", "line": 1, "character": 1}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "1. Uppercase first line [refactor]") || !strings.Contains(got, "2. Add header to b.txt [source]") {
				t.Errorf("result %q", got)
			}
		},
	},
	{
		name: "lsp_code_actions apply asks, then applies and saves",
		call: toolCall{"lsp_code_actions", map[string]any{"path": "a.txt", "line": 1, "character": 1,
			"op": "apply", "title": "uppercase first line"}},
		setup:    withFakeLSP,
		confirm:  true,
		noReview: true,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "a.txt") || !strings.Contains(got, "saved") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "a.txt") != "ALPHA\nbravo\ncharlie\n" {
				t.Errorf("a.txt %q", diskFile(d, "a.txt"))
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "ALPHA") }) {
				t.Errorf("ALPHA not on screen:\n%s", screen(d))
			}
		},
	},
	{
		name:   "lsp_rename in review mode: both files in the review list, accepted, one undo step",
		call:   toolCall{"lsp_rename", map[string]any{"path": "a.txt", "line": 1, "character": 1, "new_name": "omega"}},
		setup:  withFakeLSP,
		review: true,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "accepted the change to a.txt") || !strings.Contains(got, "accepted the change to b.txt") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "a.txt") != "omega\nbravo\ncharlie\n" || diskFile(d, "b.txt") != "omega\ntwo\nthree\n" {
				t.Errorf("disk a=%q b=%q", diskFile(d, "a.txt"), diskFile(d, "b.txt"))
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "omega") }) {
				t.Errorf("renamed text not on screen:\n%s", screen(d))
			}
			if u := lua(d, `vim.cmd('silent! undo'); return vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]`); u != "alpha" {
				t.Errorf("after undo first line %q", u)
			}
		},
	},
	{
		name: "lsp_rename in review mode refuses a tab with unsaved edits",
		call: toolCall{"lsp_rename", map[string]any{"path": "a.txt", "line": 1, "character": 1, "new_name": "omega"}},
		setup: func(d *Driver) {
			withFakeLSP(d)
			lua(d, `vim.api.nvim_buf_set_lines(0, 2, 3, false, { "mine" }); return ''`)
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "unsaved changes") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "b.txt") != "one\ntwo\nthree\n" {
				t.Errorf("b.txt changed: %q", diskFile(d, "b.txt"))
			}
			if bufLines(d) != "alpha\nbravo\nmine" {
				t.Errorf("buffer %q", bufLines(d))
			}
		},
	},
	{
		name: "lsp_code_actions apply in review mode shows the diff, then writes it",
		call: toolCall{"lsp_code_actions", map[string]any{"path": "a.txt", "line": 1, "character": 1,
			"op": "apply", "title": "uppercase first line"}},
		setup:  withFakeLSP,
		review: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "accepted the change to a.txt") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "a.txt") != "ALPHA\nbravo\ncharlie\n" {
				t.Errorf("a.txt %q", diskFile(d, "a.txt"))
			}
		},
	},
	{
		name: "lsp_code_actions apply with no matching action is refused without a dialog",
		call: toolCall{"lsp_code_actions", map[string]any{"path": "a.txt", "line": 1, "character": 1,
			"op": "apply", "title": "no such action"}},
		setup: withFakeLSP,
		check: func(t *testing.T, d *Driver, r []string) {
			got := oneResult(t, r)
			if !strings.Contains(got, "no code action matches") || !strings.Contains(got, "Uppercase first line") {
				t.Errorf("result %q", got)
			}
			if diskFile(d, "a.txt") != "alpha\nbravo\ncharlie\n" {
				t.Errorf("a.txt changed: %q", diskFile(d, "a.txt"))
			}
		},
	},
	{
		name: "lsp_hover without a language server says so",
		call: toolCall{"lsp_hover", map[string]any{"path": "x.xyz", "line": 1, "character": 1}},
		setup: func(d *Driver) {
			if err := os.WriteFile(filepath.Join(d.Dir, "x.xyz"), []byte("x\n"), 0o644); err != nil {
				d.T.Fatal(err)
			}
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if got := oneResult(t, r); !strings.Contains(got, "no language server for .xyz files") {
				t.Errorf("result %q", got)
			}
		},
	},
}
