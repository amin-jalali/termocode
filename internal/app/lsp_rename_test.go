package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// fakeRenameLSPLua starts two in-process language servers on the current
// buffer: "ren" says it wants workspace/didRenameFiles, "watch" does not.
// Both write the workspace notifications they get into _G.lsp_seen.
const fakeRenameLSPLua = `
local root = ...
_G.lsp_seen = {}
local function make(name, caps)
  return function(dispatchers)
    local closing = false
    return {
      request = function(method, params, callback)
        local res = nil
        if method == 'initialize' then res = { capabilities = caps } end
        vim.schedule(function() callback(nil, res) end)
        return true, 1
      end,
      notify = function(method, params)
        if method == 'workspace/didRenameFiles' then
          for _, f in ipairs(params.files) do
            table.insert(_G.lsp_seen, name .. ' rename ' .. f.oldUri .. ' > ' .. f.newUri)
          end
        elseif method == 'workspace/didChangeWatchedFiles' then
          for _, c in ipairs(params.changes) do
            table.insert(_G.lsp_seen, name .. ' watched ' .. c.type .. ' ' .. c.uri)
          end
        end
        return true
      end,
      is_closing = function() return closing end,
      terminate = function() closing = true end,
    }
  end
end
local ren = vim.lsp.start({ name = 'ren', cmd = make('ren', { textDocumentSync = 1, workspace = { fileOperations = {
  didRename = { filters = { { pattern = { glob = '**/*' } } } } } } }), root_dir = root })
local watch = vim.lsp.start({ name = 'watch', cmd = make('watch', { textDocumentSync = 1 }), root_dir = root })
vim.wait(2000, function()
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return #get({ bufnr = 0 }) == 2
end)
return tostring(ren) .. ',' .. tostring(watch)
`

func lspSeen(t *testing.T, m *Model) string {
	t.Helper()
	var s string
	if err := m.nvim.ExecLuaResult(`vim.wait(1000, function() return #_G.lsp_seen >= 3 end)
		local out = table.concat(_G.lsp_seen, '\n'); _G.lsp_seen = {}; return out`, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// Spec: a rename in the explorer or by the AI tells the language servers
// of that folder: workspace/didRenameFiles to a server that asks for it,
// and "old deleted, new created" (workspace/didChangeWatchedFiles) to the
// others, so their view of the files on disk stays right. A file that is
// not open is told too.
func TestRenameTellsLanguageServers(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("rename", map[string]any{"from": "c.txt", "to": "d.txt"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "c.txt": "c\n"})
	a := filepath.Join(dir, "a.txt")
	m.openFileAtLine(a, 1)
	var ids string
	if err := c.ExecLuaResult(fakeRenameLSPLua, &ids, dir); err != nil || strings.Contains(ids, "nil") {
		t.Fatalf("fake LSP: %v %q", err, ids)
	}
	uri := func(name string) string { return "file://" + filepath.Join(dir, name) }
	want := func(from, to string) string {
		return strings.Join([]string{
			"ren rename " + uri(from) + " > " + uri(to),
			"watch watched 3 " + uri(from),
			"watch watched 1 " + uri(to),
		}, "\n")
	}

	m.promptKind, m.promptPath = promptKindRename, a
	m.handlePromptSubmit("b.txt")
	if got := lspSeen(t, m); got != want("a.txt", "b.txt") {
		t.Errorf("explorer rename told:\n%s\nwant:\n%s", got, want("a.txt", "b.txt"))
	}

	driveAIChat(t, m, m.sendAIChat("rename"), nil)
	if got := lspSeen(t, m); got != want("c.txt", "d.txt") {
		t.Errorf("AI rename (file not open) told:\n%s\nwant:\n%s", got, want("c.txt", "d.txt"))
	}
}
