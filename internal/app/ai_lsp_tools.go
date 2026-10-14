package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// AI tools for the language server (Group K5):
//   - lsp_hover (auto): hover text at a position.
//   - lsp_symbols (auto): document symbols of a file, or workspace
//     symbols for a query; compact and capped.
//   - lsp_rename (fileWrite): rename the symbol at a position.
//   - lsp_code_actions (list: auto, apply: fileWrite): code actions at a
//     position.
//
// Edits go through nvim (vim.lsp.util.apply_workspace_edit), so open tabs
// show them and the user can undo them. A changed file is saved only when
// it had no unsaved user edits before (the format rule); files the edit
// had to load are saved and dropped again, so no new tab shows up. After
// an edit the IDE refreshes the explorer and git state.

// aiLSPTools are the K5 tools; nil without nvim.
func (m *Model) aiLSPTools(root string, call aiIDECaller) []ai.Tool {
	if m.nvim == nil {
		return nil
	}
	client := m.nvim
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
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
	pos := func(a map[string]any) (int, int) { return ai.ArgInt(a, "line") - 1, ai.ArgInt(a, "character") - 1 }
	posParams := func() map[string]any {
		return map[string]any{"path": str("file path"), "line": num("1-based line"), "character": num("1-based column")}
	}
	where := func(a map[string]any) string {
		return fmt.Sprintf("%s:%d:%d", ai.ArgString(a, "path"), ai.ArgInt(a, "line"), ai.ArgInt(a, "character"))
	}
	// edited formats an edit result and refreshes the IDE.
	edited := func(ctx context.Context, what, out string) (string, error) {
		res, changed := aiLSPEditSummary(what, out, rel)
		if changed {
			res += aiSyncAfter(ctx, call, "", aiSyncDisk)
		}
		return res, nil
	}

	actionParams := posParams()
	actionParams["op"] = map[string]any{"type": "string", "enum": []string{"list", "apply"}, "description": "list (default) or apply"}
	actionParams["index"] = num("apply: 1-based number from the list")
	actionParams["title"] = str("apply: action title (exact, or a unique part of it)")
	codeActions := func(a map[string]any, op string) (string, string, error) {
		abs, err := resolve(a)
		if err != nil {
			return "", "", err
		}
		l, c := pos(a)
		out, err := lua(aiLSPCodeActionsLua, abs, root, l, c, op, ai.ArgInt(a, "index"), ai.ArgString(a, "title"))
		if err != nil {
			return "", abs, err
		}
		res, err := aiLSPResult(abs, "code actions", out)
		return res, abs, err
	}

	return []ai.Tool{
		{
			Name:        "lsp_hover",
			Description: "Hover information (type, signature, docs) of the symbol at a file position, via the language server.",
			Params:      posParams(), Required: []string{"path", "line", "character"},
			Describe: func(a map[string]any) string { return "LSP hover at " + where(a) },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				l, c := pos(a)
				out, err := lua(aiLSPHoverLua, abs, root, l, c)
				if err != nil {
					return "", err
				}
				return aiLSPResult(abs, "hover", out)
			},
		},
		{
			Name: "lsp_symbols",
			Description: "Symbols via the language server: with path, the outline of that file (name, kind, line); " +
				"with query, matching symbols in the whole workspace (name, kind, file:line).",
			Params: map[string]any{"path": str("file path (document symbols)"), "query": str("symbol name to search in the workspace")},
			Describe: func(a map[string]any) string {
				if q := ai.ArgString(a, "query"); q != "" {
					return "LSP workspace symbols " + q
				}
				return "LSP symbols of " + ai.ArgString(a, "path")
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				query := ai.ArgString(a, "query")
				abs := ""
				if ai.ArgString(a, "path") != "" {
					p, err := resolve(a)
					if err != nil {
						return "", err
					}
					abs = p
				} else if query == "" {
					return "", errors.New("give a path (file outline) or a query (workspace search)")
				}
				out, err := lua(aiLSPSymbolsLua, abs, root, query)
				if err != nil {
					return "", err
				}
				return aiLSPResult(abs, "symbols", out)
			},
		},
		{
			Name: "lsp_rename",
			Description: "Rename the symbol at a file position in the whole workspace via the language server. " +
				"Open tabs show the change (undo works); changed files are saved unless they had unsaved user edits.",
			Params: func() map[string]any {
				p := posParams()
				p["new_name"] = str("the new name")
				return p
			}(),
			Required: []string{"path", "line", "character", "new_name"},
			Mutating: true, Class: ai.ClassFileWrite,
			Describe: func(a map[string]any) string {
				return fmt.Sprintf("Rename symbol at %s to %q", where(a), ai.ArgString(a, "new_name"))
			},
			Validate: func(a map[string]any) error {
				if strings.TrimSpace(ai.ArgString(a, "new_name")) == "" {
					return errors.New("new_name is required")
				}
				return nil
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := resolve(a)
				if err != nil {
					return "", err
				}
				l, c := pos(a)
				out, err := lua(aiLSPRenameLua, abs, root, l, c, ai.ArgString(a, "new_name"))
				if err != nil {
					return "", err
				}
				if out, err = aiLSPResult(abs, "rename", out); err != nil {
					return "", err
				}
				return edited(ctx, "renamed", out)
			},
		},
		{
			Name: "lsp_code_actions",
			Description: "Code actions (quick fixes, refactors) at a file position via the language server. op=list shows them " +
				"numbered; op=apply runs one by index or title. Changed files are saved unless they had unsaved user edits.",
			Params: actionParams, Required: []string{"path", "line", "character"},
			Describe: func(a map[string]any) string {
				if ai.ArgString(a, "op") != "apply" {
					return "LSP code actions at " + where(a)
				}
				which := ai.ArgString(a, "title")
				if which == "" {
					which = fmt.Sprintf("#%d", ai.ArgInt(a, "index"))
				}
				return fmt.Sprintf("Apply code action %s at %s", which, where(a))
			},
			Gate: func(a map[string]any) (bool, string) {
				if ai.ArgString(a, "op") == "apply" {
					return true, ai.ClassFileWrite
				}
				return false, ""
			},
			// Apply is checked before the dialog: the user is never asked
			// about an action that does not exist.
			Validate: func(a map[string]any) error {
				switch ai.ArgString(a, "op") {
				case "", "list":
					return nil
				case "apply":
					if ai.ArgInt(a, "index") <= 0 && ai.ArgString(a, "title") == "" {
						return errors.New("apply needs index or title (run op=list first)")
					}
					_, _, err := codeActions(a, "check")
					return err
				default:
					return errors.New("op must be list or apply")
				}
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if ai.ArgString(a, "op") != "apply" {
					res, _, err := codeActions(a, "list")
					return res, err
				}
				out, _, err := codeActions(a, "apply")
				if err != nil {
					return "", err
				}
				return edited(ctx, "applied the code action", out)
			},
		},
	}
}

// aiLSPResult turns the Lua tools' error markers into clear errors:
// NOLSP<TAB>filetype, NOCAP<TAB>client names, ERR<TAB>message.
func aiLSPResult(path, what, out string) (string, error) {
	kind, rest, _ := strings.Cut(out, "\t")
	switch kind {
	case "NOLSP":
		return "", aiNoLSPError(path, rest)
	case "NOCAP":
		return "", fmt.Errorf("the language server (%s) does not support %s", rest, what)
	case "ERR":
		return "", errors.New(rest)
	}
	return out, nil
}

// aiNoLSPError says no language server is attached and how to get one.
func aiNoLSPError(path, ft string) error {
	if path == "" {
		return errors.New("no language server is running; give a path to a source file")
	}
	what := ft + " files"
	if ext := filepath.Ext(path); ext != "" {
		what = ext + " files"
	} else if ft == "" {
		what = filepath.Base(path)
	}
	msg := "no language server for " + what
	var names []string
	for _, t := range lspinstall.ForFiletype(lspinstall.CategoryLSP, ft) {
		if ft == "" {
			break
		}
		if lspinstall.Available(t) {
			return errors.New(msg + "; " + t.Name + " is installed but did not start for this file")
		}
		names = append(names, t.Name)
	}
	if len(names) > 0 {
		return errors.New(msg + `; install one with the command "LSP: Manage Language Servers..." (` +
			strings.Join(names, " or ") + "); ask the user, the AI can not install software")
	}
	return errors.New(msg + "; termocode has no installer for this file type")
}

// aiLSPEditSummary formats the edit report lines of aiLSPEditLua
// ("saved|kept|failed<TAB>path<TAB>edits") for the model. changed is
// false when nothing changed.
func aiLSPEditSummary(what, out string, rel func(string) string) (string, bool) {
	var lines []string
	edits := 0
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(l, "\t")
		if len(f) != 3 {
			continue
		}
		n := 0
		_, _ = fmt.Sscan(f[2], &n)
		edits += n
		state := "saved"
		switch f[0] {
		case "kept":
			state = "not saved: the tab has unsaved user edits"
		case "failed":
			state = "not saved: write failed"
		}
		count := ""
		if n > 0 {
			count = fmt.Sprintf("%d edit%s, ", n, plural(n))
		}
		lines = append(lines, fmt.Sprintf("- %s (%s%s)", rel(f[1]), count, state))
	}
	if len(lines) == 0 {
		return "the language server returned no changes", false
	}
	head := fmt.Sprintf("%s: %d file%s changed", what, len(lines), plural(len(lines)))
	if edits > 0 {
		head += fmt.Sprintf(" (%d edit%s)", edits, plural(edits))
	}
	return head + "; open tabs show it and the user can undo it\n" + strings.Join(lines, "\n"), true
}

// aiLSPPrelude: helpers shared by the K5 Lua chunks.
//   - lspbuf(path): buffer + attached clients; a buffer loaded just now
//     gets a moment for its server to attach.
//   - capable(clients, cap): clients whose server has capability cap.
//   - req(client, method, params, buf): one synchronous request.
//   - rel(fname): path relative to the workspace root.
const aiLSPPrelude = aiBufForPath + `
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local function lspbuf(path)
  local old = vim.fn.bufnr(path)
  local fresh = old == -1 or not vim.api.nvim_buf_is_loaded(old)
  local b, clients = bufclients(path)
  if #clients == 0 and fresh then
    vim.wait(1500, function() return #get({ bufnr = b }) > 0 end, 50)
    clients = get({ bufnr = b })
  end
  return b, clients
end
local function capable(clients, cap)
  local out, names = {}, {}
  for _, c in ipairs(clients) do
    names[#names + 1] = c.name
    if (c.server_capabilities or {})[cap] then out[#out + 1] = c end
  end
  return out, table.concat(names, ', ')
end
local function req(c, method, params, b)
  local ok, r = pcall(function() return c:request_sync(method, params, 3000, b) end)
  if ok and r then return r end
  ok, r = pcall(c.request_sync, method, params, 3000, b)
  if ok then return r end
end
local function relp(root, f)
  if f:sub(1, #root + 1) == root .. '/' then return f:sub(#root + 2) end
  return f
end
`

const aiLSPHoverLua = aiLSPPrelude + `
local path, root, line, ch = ...
local b, clients = lspbuf(path)
if #clients == 0 then return 'NOLSP\t' .. vim.bo[b].filetype end
local cs, names = capable(clients, 'hoverProvider')
if #cs == 0 then return 'NOCAP\t' .. names end
local params = { textDocument = { uri = vim.uri_from_fname(path) }, position = { line = line, character = math.max(ch, 0) } }
local function text(c)
  if type(c) == 'string' then return c end
  if type(c) ~= 'table' then return '' end
  if c.value then
    if c.language then return '` + "```" + `' .. c.language .. '\n' .. c.value .. '\n` + "```" + `' end
    return c.value
  end
  local parts = {}
  for _, x in ipairs(c) do parts[#parts + 1] = text(x) end
  return table.concat(parts, '\n')
end
local out = {}
for _, c in ipairs(cs) do
  local r = req(c, 'textDocument/hover', params, b)
  if r and r.result and r.result.contents then
    local t = vim.trim(text(r.result.contents))
    if t ~= '' then out[#out + 1] = t end
  end
end
if #out == 0 then return 'no hover information at this position' end
local s = table.concat(out, '\n---\n')
if #s > 4000 then s = s:sub(1, 4000) .. '\n…(cut)' end
return s
`

const aiLSPSymbolsLua = aiLSPPrelude + `
local path, root, query = ...
local kinds = vim.lsp.protocol.SymbolKind or {}
local b, clients
if path ~= '' then
  b, clients = lspbuf(path)
  if #clients == 0 then return 'NOLSP\t' .. vim.bo[b].filetype end
else
  clients = get()
  if #clients == 0 then return 'NOLSP\t' end
end
local out, cap = {}, 200
if query ~= '' then
  local cs, names = capable(clients, 'workspaceSymbolProvider')
  if #cs == 0 then return 'NOCAP\t' .. names end
  for _, c in ipairs(cs) do
    local cb = b
    if not cb then for x in pairs(c.attached_buffers or {}) do cb = x break end end
    local r = req(c, 'workspace/symbol', { query = query }, cb)
    for _, s in ipairs((r and r.result) or {}) do
      if #out >= 100 then break end
      local loc = s.location or {}
      local where = ''
      if loc.uri then
        where = ' ' .. relp(root, vim.uri_to_fname(loc.uri))
        if loc.range then where = where .. ':' .. (loc.range.start.line + 1) end
      end
      out[#out + 1] = s.name .. ' (' .. (kinds[s.kind] or '?') .. ')' .. where
    end
  end
  if #out == 0 then return 'no workspace symbols match ' .. query end
  return table.concat(out, '\n')
end
local cs, names = capable(clients, 'documentSymbolProvider')
if #cs == 0 then return 'NOCAP\t' .. names end
local function walk(list, depth)
  for _, s in ipairs(list or {}) do
    if #out >= cap then return end
    local rg = s.selectionRange or s.range or (s.location and s.location.range)
    local line = rg and (' line ' .. (rg.start.line + 1)) or ''
    out[#out + 1] = string.rep('  ', depth) .. s.name .. ' (' .. (kinds[s.kind] or '?') .. ')' .. line
    walk(s.children, depth + 1)
  end
end
local r = req(cs[1], 'textDocument/documentSymbol', { textDocument = { uri = vim.uri_from_fname(path) } }, b)
walk(r and r.result, 0)
if #out == 0 then return 'no symbols in ' .. relp(root, path) end
if #out >= cap then out[#out + 1] = '…(first ' .. cap .. ' symbols)' end
return table.concat(out, '\n')
`

// aiLSPEditLua: snapshot() before an edit, settle(before, counts) after
// it saves what changed (only buffers with no unsaved user edits
// before), drops buffers the edit loaded, and reports one line per
// changed file: "saved|kept|failed<TAB>path<TAB>edits".
const aiLSPEditLua = aiLSPPrelude + aiLSPPreviewLua + `
local function snapshot()
  local s = {}
  for _, x in ipairs(vim.api.nvim_list_bufs()) do
    s[x] = { loaded = vim.api.nvim_buf_is_loaded(x), listed = vim.bo[x].buflisted,
      modified = vim.bo[x].modified, tick = vim.api.nvim_buf_get_changedtick(x) }
  end
  return s
end
local function count_edits(edit, counts)
  if type(edit) ~= 'table' then return end
  for uri, edits in pairs(edit.changes or {}) do
    local f = vim.uri_to_fname(uri)
    counts[f] = (counts[f] or 0) + #edits
  end
  for _, dc in ipairs(edit.documentChanges or {}) do
    local uri = (dc.textDocument and dc.textDocument.uri) or dc.newUri or dc.uri
    if uri then
      local f = vim.uri_to_fname(uri)
      counts[f] = (counts[f] or 0) + (dc.edits and #dc.edits or 1)
    end
  end
end
local function settle(before, counts)
  local out = {}
  for _, x in ipairs(vim.api.nvim_list_bufs()) do
    local name = vim.api.nvim_buf_get_name(x)
    if vim.api.nvim_buf_is_loaded(x) and vim.bo[x].buftype == '' and name ~= '' then
      local was = before[x]
      local had = was and was.loaded
      local changed
      if had then changed = vim.api.nvim_buf_get_changedtick(x) ~= was.tick else changed = vim.bo[x].modified end
      if changed then
        local state = 'kept'
        if not (had and was.modified) then
          vim.api.nvim_buf_call(x, function() vim.cmd('silent! write') end)
          state = vim.bo[x].modified and 'failed' or 'saved'
        end
        out[#out + 1] = state .. '\t' .. name .. '\t' .. (counts[name] or 0)
      end
      -- A buffer the edit loaded only to change it: drop it again, so no
      -- tab shows up for it.
      if not vim.bo[x].modified then
        if not had then
          pcall(vim.api.nvim_buf_delete, x, {})
        elseif not was.listed then
          vim.bo[x].buflisted = false
        end
      end
    end
  end
  return table.concat(out, '\n')
end
`

const aiLSPRenameLua = aiLSPEditLua + `
local path, root, line, ch, newname, op = ...
local before = snapshot()
local b, clients = lspbuf(path)
if #clients == 0 then return 'NOLSP\t' .. vim.bo[b].filetype end
local cs, names = capable(clients, 'renameProvider')
if #cs == 0 then return 'NOCAP\t' .. names end
local c = cs[1]
local params = { textDocument = { uri = vim.uri_from_fname(path) }, position = { line = line, character = math.max(ch, 0) }, newName = newname }
local r = req(c, 'textDocument/rename', params, b)
if not r then return 'ERR\tthe language server (' .. c.name .. ') did not answer' end
if r.err then return 'ERR\trename failed: ' .. (r.err.message or vim.inspect(r.err)) end
if not r.result then return 'ERR\tnothing to rename at this position' end
if op == 'preview' then return preview(r.result, c.offset_encoding) end
local counts = {}
count_edits(r.result, counts)
vim.lsp.util.apply_workspace_edit(r.result, c.offset_encoding or 'utf-16')
return settle(before, counts)
`

// aiLSPCodeActionsLua: op list (numbered titles), check (does index /
// title match one action?) or apply.
const aiLSPCodeActionsLua = aiLSPEditLua + `
local path, root, line, ch, op, index, title = ...
local before = snapshot()
local b, clients = lspbuf(path)
if #clients == 0 then return 'NOLSP\t' .. vim.bo[b].filetype end
local cs, names = capable(clients, 'codeActionProvider')
if #cs == 0 then return 'NOCAP\t' .. names end
local diags = {}
for _, d in ipairs(vim.diagnostic.get(b, { lnum = line })) do
  diags[#diags + 1] = {
    range = { start = { line = d.lnum, character = d.col }, ['end'] = { line = d.end_lnum or d.lnum, character = d.end_col or d.col } },
    severity = d.severity, code = d.code, source = d.source, message = d.message,
  }
end
local p = { line = line, character = math.max(ch, 0) }
local params = { textDocument = { uri = vim.uri_from_fname(path) }, range = { start = p, ['end'] = p },
  context = { diagnostics = diags, triggerKind = 1 } }
local actions = {}
for _, c in ipairs(cs) do
  local r = req(c, 'textDocument/codeAction', params, b)
  for _, a in ipairs((r and r.result) or {}) do actions[#actions + 1] = { client = c, action = a } end
end
local list = {}
for i, e in ipairs(actions) do
  local a = e.action
  local kind = (a.kind and a.kind ~= '') and (' [' .. a.kind .. ']') or ''
  list[#list + 1] = i .. '. ' .. (a.title or '?') .. kind .. (a.isPreferred and ' (preferred)' or '')
end
if op == 'list' then
  if #list == 0 then return 'no code actions at this position' end
  return table.concat(list, '\n')
end
local pick
if index and index > 0 then
  pick = actions[index]
elseif title ~= '' then
  local want = title:lower()
  local part = {}
  for _, e in ipairs(actions) do
    local t = (e.action.title or ''):lower()
    if t == want then pick = e break end
    if t:find(want, 1, true) then part[#part + 1] = e end
  end
  if not pick and #part == 1 then pick = part[1] end
end
if not pick then
  local avail = #list > 0 and ('; available:\n' .. table.concat(list, '\n')) or '; there are no code actions at this position'
  return 'ERR\tno code action matches' .. avail
end
if op == 'check' then return 'ok' end
local c, a = pick.client, pick.action
if a.edit == nil and a.command == nil or (type(a.command) ~= 'string' and a.edit == nil and a.data ~= nil) then
  local caps = (c.server_capabilities or {}).codeActionProvider
  if type(caps) == 'table' and caps.resolveProvider then
    local r = req(c, 'codeAction/resolve', a, b)
    if r and r.result then a = r.result end
  end
end
if op == 'preview' then
  if a.command or a.edit == nil then return 'CMD\t' end
  return preview(a.edit, c.offset_encoding)
end
local counts = {}
if a.edit then
  count_edits(a.edit, counts)
  vim.lsp.util.apply_workspace_edit(a.edit, c.offset_encoding or 'utf-16')
end
local cmd = a.command
if type(cmd) == 'string' then cmd = { command = a.command, arguments = a.arguments } end
if type(cmd) == 'table' and cmd.command then
  local r = req(c, 'workspace/executeCommand', { command = cmd.command, arguments = cmd.arguments }, b)
  if r and r.err then return 'ERR\tthe action command failed: ' .. (r.err.message or vim.inspect(r.err)) end
end
return settle(before, counts)
`
