package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// fakeFormatterLua attaches an in-process language server to the current
// buffer whose formatter puts "FMT " at the start of the first line.
const fakeFormatterLua = `
local root = ...
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { documentFormattingProvider = true, textDocumentSync = 1 } }
      elseif method == 'textDocument/formatting' then
        res = { { range = { start = { line = 0, character = 0 }, ['end'] = { line = 0, character = 0 } }, newText = 'FMT ' } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function() return true end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fakefmt', cmd = server, root_dir = root })
vim.wait(2000, function()
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return #get({ bufnr = 0 }) > 0
end)
return tostring(id)
`

// Spec: format on an open file with no unsaved edits formats and saves;
// with unsaved user edits it formats in the editor but does not save
// (that would write the user's half-done edits), and says so.
func TestAIFormatToolSavesOnlyWithoutUnsavedEdits(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		name := "clean"
		if dirty {
			name = "unsaved"
		}
		t.Run(name, func(t *testing.T) {
			p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("format", map[string]any{"path": "a.txt"})}}
			m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "one\n"})
			path := filepath.Join(dir, "a.txt")
			m.openFileAtLine(path, 1)
			var id string
			if err := c.ExecLuaResult(fakeFormatterLua, &id, dir); err != nil || id == "nil" {
				t.Fatalf("fake LSP: %v %q", err, id)
			}
			if dirty {
				_ = c.Command("normal! Amine")
			}
			driveAIChat(t, m, m.sendAIChat("format it"), nil)
			r := oneResult(t, p)
			if dirty {
				if got := bufText(t, c, path); got != "FMT onemine" || !bufModified(t, c, path) {
					t.Errorf("buffer %q modified=%v", got, bufModified(t, c, path))
				}
				if readFile(path) != "one\n" || !strings.Contains(r, "not saved") {
					t.Errorf("disk %q result %q: the user's edits were saved", readFile(path), r)
				}
				return
			}
			if readFile(path) != "FMT one\n" || bufModified(t, c, path) || !strings.Contains(r, "formatted and saved") {
				t.Errorf("disk %q modified=%v result %q", readFile(path), bufModified(t, c, path), r)
			}
		})
	}
}
