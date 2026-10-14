package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/search"
)

// K9: what still pointed at old files / lines after the agent (or the
// explorer) changed them.

// pumpFind runs cmd and feeds every Find in Files batch it yields to
// handleFindBatch until the search is done. Other messages are dropped.
func pumpFind(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("cmd did not finish in 10s")
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			pumpFind(t, m, c)
		}
	case findBatchMsg:
		pumpFind(t, m, m.handleFindBatch(msg))
	}
}

// findResultsText is the text of the Find Results buffer.
func findResultsText(t *testing.T, m *Model) string {
	t.Helper()
	s, err := m.nvim.EvalLuaString(`local b = vim.g.termocode_find_results_buf
		if not b or not vim.api.nvim_buf_is_valid(b) then return '<none>' end
		return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Spec: Find in Files results follow the agent's changes. After a tool
// changed files on disk, the open Find Results buffer is searched again
// in place: new lines, gone files gone, and the user's current window
// stays where it is (also when the results are not on screen).
func TestK9FindResultsRefreshAfterAIChange(t *testing.T) {
	m, c, dir := aiSyncModel(t, &scriptedAIProvider{}, map[string]string{
		"a.txt": "needle\n", "b.txt": "x\nneedle\n", "c.txt": "nothing\n"})
	pumpFind(t, m, m.runFindInFiles("needle"))
	before := findResultsText(t, m)
	if !strings.Contains(before, "a.txt (1):") || !strings.Contains(before, "b.txt (1):") {
		t.Fatalf("first search:\n%s", before)
	}
	a := filepath.Join(dir, "a.txt")
	m.openFileAtLine(a, 1) // the user goes back to a file

	// The agent moves the match in a.txt, removes b.txt, adds one to c.txt.
	_ = os.WriteFile(a, []byte("one\ntwo\nneedle here\n"), 0o644)
	_ = os.Remove(filepath.Join(dir, "b.txt"))
	_ = os.WriteFile(filepath.Join(dir, "c.txt"), []byte("needle in c\n"), 0o644)
	_, cmd := m.aiSyncIDE("", aiSyncDisk)
	pumpFind(t, m, cmd)

	after := findResultsText(t, m)
	if strings.Contains(after, "b.txt") || !strings.Contains(after, "c.txt (1):") ||
		!strings.Contains(after, "   3:    needle here") {
		t.Errorf("results not refreshed:\n%s", after)
	}
	if name, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`); name != a {
		t.Errorf("current buffer %q: the refresh moved the user's window", name)
	}
}

// Spec: with no Find Results buffer (never searched, or closed) a change
// does not start a search.
func TestK9NoFindRefreshWithoutResults(t *testing.T) {
	m, _, _ := aiSyncModel(t, &scriptedAIProvider{}, map[string]string{"a.txt": "needle\n"})
	m.findRun.query = "needle" // a search ran once, its buffer is gone
	before := m.findRun.gen
	m.aiSyncIDE("", aiSyncDisk)
	if m.findRun.gen != before {
		t.Error("a search started for a closed Find Results buffer")
	}
}

// Spec: the Find in Files overlay, when open, searches again after the
// agent changed files.
func TestK9SearchOverlayRefreshAfterAIChange(t *testing.T) {
	m, _, _ := aiSyncModel(t, &scriptedAIProvider{}, map[string]string{"a.txt": "needle\n"})
	m.search = search.New()
	m.searchOpen = true
	m.search, _ = m.search.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
	if _, cmd := m.aiSyncIDE("", aiSyncDisk); cmd == nil || !m.search.Pending() {
		t.Error("the open search overlay was not searched again")
	}
}

// k9Env points recents and hot-exit backups at a temp home.
func k9Env(t *testing.T, m *Model) (prev string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	m.hotExitRoot = filepath.Join(home, "state", "backup", "ws")
	return filepath.Join(m.hotExitRoot, "previous")
}

// k9Backup writes a hot-exit backup of path (as hotExitLua names it).
func k9Backup(t *testing.T, prev, path, text string) {
	t.Helper()
	_ = os.MkdirAll(prev, 0o700)
	sum := sha256.Sum256([]byte(path))
	name := hex.EncodeToString(sum[:])[:16] + ".bak"
	if err := os.WriteFile(filepath.Join(prev, name), []byte(path+"\n"+text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func k9BackupPaths(t *testing.T, prev string) []string {
	t.Helper()
	files, _ := os.ReadDir(prev)
	var out []string
	for _, b := range readHotBackups(prev, files) {
		out = append(out, b.Path)
	}
	slices.Sort(out)
	return out
}

func k9RecentPaths() []string {
	var out []string
	for _, e := range loadRecents() {
		out = append(out, e.Path)
	}
	return out
}

// k9Seed records a.txt and dir/x.txt as closed tabs, recent files and
// hot-exit backups.
func k9Seed(t *testing.T, m *Model, dir, prev string) (a, x string) {
	t.Helper()
	a, x = filepath.Join(dir, "a.txt"), filepath.Join(dir, "d", "x.txt")
	for _, p := range []string{a, x} {
		m.pushClosed(p)
		pushRecent(p)
		k9Backup(t, prev, p, "unsaved")
	}
	return a, x
}

func k9Check(t *testing.T, m *Model, prev string, want ...string) {
	t.Helper()
	slices.Sort(want)
	ring := slices.Clone(m.closedRing)
	slices.Sort(ring)
	if !slices.Equal(ring, want) {
		t.Errorf("closed tabs %v, want %v", ring, want)
	}
	rec := k9RecentPaths()
	slices.Sort(rec)
	if !slices.Equal(rec, want) {
		t.Errorf("recents %v, want %v", rec, want)
	}
	if got := k9BackupPaths(t, prev); !slices.Equal(got, want) {
		t.Errorf("hot-exit backups %v, want %v", got, want)
	}
}

// Spec: an agent rename moves the closed-tabs list, the recent files and
// the hot-exit backups to the new path (file and folder).
func TestK9AIRenameMovesClosedRecentsBackups(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("rename", map[string]any{"from": "a.txt", "to": "b.txt"}),
		toolCall("rename", map[string]any{"from": "d", "to": "e"}),
	}}
	m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "d/x.txt": "x\n"})
	prev := k9Env(t, m)
	k9Seed(t, m, dir, prev)
	driveAIChat(t, m, m.sendAIChat("rename"), nil)
	k9Check(t, m, prev, filepath.Join(dir, "b.txt"), filepath.Join(dir, "e", "x.txt"))
	// The backup is stored under the new path's name, so a later live
	// backup of the renamed file replaces it instead of adding a second.
	k9Backup(t, prev, filepath.Join(dir, "b.txt"), "newer")
	if got := k9BackupPaths(t, prev); len(got) != 2 {
		t.Errorf("backups %v: the moved one kept the old file name", got)
	}
}

// Spec: an agent delete drops the closed tabs, recent files and hot-exit
// backups of what it deleted, also the tab it closed itself.
func TestK9AIDeleteDropsClosedRecentsBackups(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		toolCall("delete", map[string]any{"path": "a.txt"}),
		toolCall("delete", map[string]any{"path": "d"}),
	}}
	m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "d/x.txt": "x\n", "keep.txt": "k\n"})
	prev := k9Env(t, m)
	k9Seed(t, m, dir, prev)
	keep := filepath.Join(dir, "keep.txt")
	m.pushClosed(keep)
	pushRecent(keep)
	k9Backup(t, prev, keep, "unsaved")
	m.openFileAtLine(filepath.Join(dir, "d", "x.txt"), 1)
	driveAIChat(t, m, m.sendAIChat("delete"), nil)
	k9Check(t, m, prev, keep)
}

// Spec: the explorer's rename and delete do the same.
func TestK9ExplorerRenameDeleteFollow(t *testing.T) {
	m, _, dir := aiSyncModel(t, &scriptedAIProvider{}, map[string]string{"a.txt": "a\n", "d/x.txt": "x\n"})
	prev := k9Env(t, m)
	a, x := k9Seed(t, m, dir, prev)
	m.promptKind, m.promptPath = promptKindRename, a
	m.handlePromptSubmit("b.txt")
	k9Check(t, m, prev, filepath.Join(dir, "b.txt"), x)

	m.confirmKind, m.confirmTargetPath, m.confirmTargetPaths = confirmKindDeletePath, filepath.Join(dir, "d"), nil
	m.handleConfirmSelect("delete")
	k9Check(t, m, prev, filepath.Join(dir, "b.txt"))

	m.confirmKind, m.confirmTargetPath, m.confirmTargetPaths = confirmKindDeletePath, "", []string{filepath.Join(dir, "b.txt")}
	m.handleConfirmSelect("delete")
	k9Check(t, m, prev)
}

// fakeK9LSPLua attaches an in-process language server to the current
// buffer. It knows the documents opened to it (didOpen / didClose),
// answers hover only for those and publishes one diagnostic per opened
// document.
const fakeK9LSPLua = `
local root = ...
_G.k9_open = {}
local function server(dispatchers)
  local closing = false
  return {
    request = function(method, params, callback)
      local res = nil
      if method == 'initialize' then
        res = { capabilities = { hoverProvider = true, textDocumentSync = 1 } }
      elseif method == 'textDocument/hover' and _G.k9_open[params.textDocument.uri] then
        res = { contents = { kind = 'plaintext', value = 'HOVER ' .. params.textDocument.uri } }
      end
      vim.schedule(function() callback(nil, res) end)
      return true, 1
    end,
    notify = function(method, params)
      if method == 'textDocument/didOpen' then
        local uri = params.textDocument.uri
        _G.k9_open[uri] = true
        vim.schedule(function()
          dispatchers.notification('textDocument/publishDiagnostics', { uri = uri, diagnostics = {
            { range = { start = { line = 0, character = 0 }, ['end'] = { line = 0, character = 1 } },
              message = 'K9DIAG', severity = 1 } } })
        end)
      elseif method == 'textDocument/didClose' then
        _G.k9_open[params.textDocument.uri] = nil
      end
      return true
    end,
    is_closing = function() return closing end,
    terminate = function() closing = true end,
  }
end
local id = vim.lsp.start({ name = 'fakek9', cmd = server, root_dir = root })
vim.wait(2000, function()
  local get = vim.lsp.get_clients or vim.lsp.get_active_clients
  return #get({ bufnr = 0 }) > 0
end)
return tostring(id)
`

// k9LSPState reports, for the buffer of path: attached clients, whether
// the server has the file open (and the old one closed), the hover text
// and the diagnostics.
const k9LSPStateLua = `
local path, old = ...
local b = vim.fn.bufnr(path)
if b < 0 then return 'no buffer' end
local uri, olduri = vim.uri_from_fname(path), vim.uri_from_fname(old)
vim.wait(2000, function()
  local d = vim.diagnostic.get(b)
  return _G.k9_open[uri] and #d > 0
end)
local hover = '<none>'
local res = vim.lsp.buf_request_sync(b, 'textDocument/hover',
  { textDocument = { uri = uri }, position = { line = 0, character = 0 } }, 2000)
for _, r in pairs(res or {}) do
  if r.result and r.result.contents then hover = r.result.contents.value end
end
local msgs = {}
for _, d in ipairs(vim.diagnostic.get(b)) do msgs[#msgs + 1] = d.message end
return string.format('clients=%d open=%s oldopen=%s hover=%s diags=%s',
  #vim.lsp.get_clients({ bufnr = b }), tostring(_G.k9_open[uri] == true),
  tostring(_G.k9_open[olduri] == true), hover, table.concat(msgs, ','))
`

// Spec: after an agent rename of an open file, the language server stays
// attached to the renamed buffer and knows the new path: hover and
// diagnostics work there, and the old path is closed on the server.
func TestK9AIRenameKeepsLSP(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("rename", map[string]any{"from": "a.txt", "to": "b.txt"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "hello\n"})
	oldP, newP := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	m.openFileAtLine(oldP, 1)
	var id string
	if err := c.ExecLuaResult(fakeK9LSPLua, &id, dir); err != nil || id == "nil" {
		t.Fatalf("fake LSP: %v %q", err, id)
	}
	driveAIChat(t, m, m.sendAIChat("rename"), nil)
	var st string
	if err := c.ExecLuaResult(k9LSPStateLua, &st, newP, oldP); err != nil {
		t.Fatal(err)
	}
	want := "clients=1 open=true oldopen=false hover=HOVER file://" + newP + " diags=K9DIAG"
	if st != want {
		t.Errorf("LSP after rename:\n got %s\nwant %s", st, want)
	}
}

// Spec: execute_action save (the agent) refreshes git status like Ctrl+S:
// the action's command fetches git.
func TestK9AIExecuteSaveRefreshesGit(t *testing.T) {
	m, _, dir := aiSyncModel(t, &scriptedAIProvider{}, map[string]string{"a.txt": "a\n"})
	m.openFileAtLine(filepath.Join(dir, "a.txt"), 1)
	for _, id := range []string{"save", "save-all"} {
		cmd := m.dispatchPaletteAction(id)
		if !cmdYields[GitMsg](t, cmd) {
			t.Errorf("%s: no git refresh", id)
		}
	}
}

// cmdYields runs cmd (and the cmds of a batch) and reports whether one of
// them produced a T.
func cmdYields[T any](t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case T:
		return true
	case tea.BatchMsg:
		for _, c := range msg {
			if cmdYields[T](t, c) {
				return true
			}
		}
	}
	return false
}
