package app

import (
	"context"
	"errors"
	"fmt"

	"termocode/internal/ai"
)

// aiEditorTools are the agent tools that need the embedded nvim (LSP and
// open buffers). The filesystem / git / search / run tools live in
// internal/ai (ai.WorkspaceTools).
func (m *Model) aiEditorTools() []ai.Tool {
	if m.nvim == nil {
		return nil
	}
	client := m.nvim
	ws := ai.Workspace{Root: aiWorkspaceRoot()}
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	lua := func(code string, args ...any) (string, error) {
		var out string
		err := client.ExecLuaResult(code, &out, args...)
		return out, err
	}
	resolve := func(a map[string]any) (string, error) {
		p := ai.ArgString(a, "path")
		if p == "" {
			return "", errors.New("path is required")
		}
		return ws.Resolve(p)
	}
	return []ai.Tool{
		{
			Name:        "lsp_nav",
			Description: "Go to definition or find references of the symbol at a file position, via the language server.",
			Params: map[string]any{
				"path": str("file path"), "line": num("1-based line"), "character": num("1-based column"),
				"kind": map[string]any{"type": "string", "enum": []string{"definition", "references"}},
			},
			Required: []string{"path", "line", "character"},
			Describe: func(a map[string]any) string {
				k := ai.ArgString(a, "kind")
				if k == "" {
					k = "definition"
				}
				return fmt.Sprintf("LSP %s at %s:%d", k, ai.ArgString(a, "path"), ai.ArgInt(a, "line"))
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				method := "textDocument/definition"
				if ai.ArgString(a, "kind") == "references" {
					method = "textDocument/references"
				}
				return lua(aiLspNavLua, abs, method, ai.ArgInt(a, "line")-1, ai.ArgInt(a, "character")-1)
			},
		},
		{
			Name:        "diagnostics",
			Description: "List language-server diagnostics (errors / warnings) for a file, or for all open files when path is empty.",
			Params:      map[string]any{"path": str("file path (optional)")},
			Describe:    func(a map[string]any) string { return "Diagnostics " + ai.ArgString(a, "path") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs := ""
				if ai.ArgString(a, "path") != "" {
					p, err := resolve(a)
					if err != nil {
						return "", err
					}
					abs = p
				}
				return lua(aiDiagnosticsLua, abs)
			},
		},
		{
			Name:        "format",
			Description: "Format a file with its language server and save it.",
			Params:      map[string]any{"path": str("file path")}, Required: []string{"path"},
			Mutating: true, Class: ai.ClassFileWrite,
			Describe: func(a map[string]any) string { return "Format " + ai.ArgString(a, "path") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				return lua(aiFormatLua, abs)
			},
		},
	}
}

// aiBufForPath (Lua prelude): returns the buffer for a path, loading it
// when needed, plus its attached LSP clients.
const aiBufForPath = `
local function bufclients(path)
  local b = vim.fn.bufnr(path)
  if b == -1 then
    b = vim.fn.bufadd(path)
    pcall(vim.fn.bufload, b)
  end
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return b, get({ bufnr = b })
end
`

const aiLspNavLua = aiBufForPath + `
local path, method, line, ch = ...
local b, clients = bufclients(path)
if #clients == 0 then return 'no language server attached to ' .. path .. ' (open it in the editor first)' end
local params = { textDocument = { uri = vim.uri_from_fname(path) }, position = { line = line, character = math.max(ch, 0) } }
if method == 'textDocument/references' then params.context = { includeDeclaration = true } end
local res = vim.lsp.buf_request_sync(b, method, params, 3000) or {}
local out = {}
for _, r in pairs(res) do
  local locs = r.result or {}
  if locs.uri or locs.targetUri then locs = { locs } end
  for _, l in ipairs(locs) do
    local uri = l.uri or l.targetUri
    local rg = l.range or l.targetSelectionRange or l.targetRange
    if uri and rg then
      out[#out + 1] = vim.uri_to_fname(uri) .. ':' .. (rg.start.line + 1) .. ':' .. (rg.start.character + 1)
      if #out >= 50 then break end
    end
  end
end
if #out == 0 then return 'no results' end
return table.concat(out, '\n')
`

const aiDiagnosticsLua = `
local path = ...
local names = { 'error', 'warning', 'info', 'hint' }
local diags
if path ~= '' then
  local b = vim.fn.bufnr(path)
  if b == -1 then return 'file is not open; no diagnostics known' end
  diags = vim.diagnostic.get(b)
else
  diags = vim.diagnostic.get()
end
local out = {}
for _, d in ipairs(diags) do
  out[#out + 1] = vim.api.nvim_buf_get_name(d.bufnr) .. ':' .. (d.lnum + 1) .. ': [' .. (names[d.severity] or '?') .. '] ' .. (d.message or '')
  if #out >= 100 then break end
end
if #out == 0 then return 'no diagnostics' end
return table.concat(out, '\n')
`

const aiFormatLua = aiBufForPath + `
local path = ...
local b, clients = bufclients(path)
if #clients == 0 then return 'no language server attached to ' .. path .. ' (open it in the editor first)' end
local ok, err = pcall(vim.lsp.buf.format, { bufnr = b, timeout_ms = 3000 })
if not ok then return 'format failed: ' .. tostring(err) end
vim.api.nvim_buf_call(b, function() vim.cmd('silent! write') end)
return 'formatted and saved ' .. path
`
