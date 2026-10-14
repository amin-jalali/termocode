package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The IDE follows every change the agent makes, as in VS Code / Cursor:
// a file it creates shows up in the explorer at once, a renamed open file
// keeps its tab under the new name, a deleted one loses its tab, a
// written one shows the new text, and git status / branch / explorer
// markers refresh after git and shell commands.
//
// Tools run on the agent goroutine; the IDE part runs on Update through
// the aiIDECaller (see ai_ide_tools.go). write_file, rename and delete do
// the whole file operation on Update, so the check of the open buffers
// and the change on disk cannot race the user's typing.
//
// Unsaved edits are never lost: write_file and delete refuse a file whose
// editor tab has unsaved changes (the model gets an error telling it to
// ask the user); rename keeps the unsaved text in the renamed tab.

// aiRel makes paths relative to root for tool results.
func aiRel(root string) func(string) string {
	return func(abs string) string {
		if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return abs
	}
}

// aiSyncLevel says what a tool may have changed.
type aiSyncLevel int

const (
	aiSyncGit  aiSyncLevel = iota // git state only (stage, commit, push)
	aiSyncDisk                    // any file on disk (checkout, pull, shell)
)

// aiSyncTools wraps the workspace and editor tools so the IDE follows
// their changes. Other tools pass through unchanged.
func aiSyncTools(tools []ai.Tool, root string, call aiIDECaller) []ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	out := make([]ai.Tool, 0, len(tools))
	for _, t := range tools {
		inner := t.Run
		switch t.Name {
		case "write_file":
			t.Description = "Write a file with new content (creates it if missing). If the file is open in the editor, its tab shows the new text " +
				"(the user can undo it); a file with unsaved edits in the editor is refused."
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := ws.Resolve(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				content := ai.ArgString(a, "content")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiWriteFile(abs, rel(abs), content) })
			}
		case "delete":
			t.Description = "Delete a file or directory. Its editor tabs are closed; a file with unsaved edits in the editor is refused."
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := ws.Resolve(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				if r, _ := ws.Resolve(""); abs == r {
					return "", errors.New("refusing to delete the workspace root")
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiDeletePath(abs, rel) })
			}
		case "rename":
			t.Description = "Rename or move a file / directory. Open editor tabs follow to the new name (unsaved edits are kept). Fails if the target exists."
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				from, err := ws.Resolve(ai.ArgString(a, "from"))
				if err != nil {
					return "", err
				}
				to, err := ws.Resolve(ai.ArgString(a, "to"))
				if err != nil {
					return "", err
				}
				if r, _ := ws.Resolve(""); from == r {
					return "", errors.New("refusing to rename the workspace root")
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiRenamePath(from, to, rel) })
			}
		case "create_file", "create_dir":
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				res, err := inner(ctx, a)
				if err != nil {
					return res, err
				}
				abs, _ := ws.Resolve(ai.ArgString(a, "path"))
				note := aiSyncAfter(ctx, call, abs, aiSyncDisk)
				if t.Name == "create_file" {
					note += "; open it for the user with open_file when they want to see it"
				}
				return res + note, nil
			}
		case "git_stage", "git_commit", "git_push":
			t.Run = aiAfterRun(inner, call, aiSyncGit)
		case "git_checkout", "git_pull", "run_command", "format":
			t.Run = aiAfterRun(inner, call, aiSyncDisk)
		}
		out = append(out, t)
	}
	return out
}

// aiAfterRun runs inner, then refreshes the IDE (even when inner failed:
// a failed pull or command may still have changed files).
func aiAfterRun(inner func(context.Context, map[string]any) (string, error), call aiIDECaller, lvl aiSyncLevel) func(context.Context, map[string]any) (string, error) {
	return func(ctx context.Context, a map[string]any) (string, error) {
		res, err := inner(ctx, a)
		note := aiSyncAfter(ctx, call, "", lvl)
		if err != nil {
			return res, err
		}
		return res + note, nil
	}
}

// aiSyncAfter refreshes the IDE on Update and returns a note for the
// model ("" when there is nothing to say).
func aiSyncAfter(ctx context.Context, call aiIDECaller, reveal string, lvl aiSyncLevel) string {
	note, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		n, cmd := m.aiSyncIDE(reveal, lvl)
		return n, cmd, nil
	})
	if err != nil {
		return ""
	}
	return note
}

// aiSyncIDE refreshes what changed: for aiSyncDisk the open buffers
// (checktime), the explorer tree (revealing reveal) and the Find in Files
// results; always git status (git panel, branch, explorer markers) and
// the tab bar.
func (m *Model) aiSyncIDE(reveal string, lvl aiSyncLevel) (string, tea.Cmd) {
	note := ""
	var search tea.Cmd
	if lvl == aiSyncDisk {
		search = m.refreshSearchResults()
		m.explorer.Reload()
		if reveal != "" {
			m.explorer.RevealPath(reveal)
			if m.explorer.CursorPath() == reveal {
				note = "; it is shown in the explorer"
			} else {
				note = "; the explorer hides it (hidden or ignored file)"
			}
		}
		note += m.aiCheckTime()
	}
	return note, tea.Batch(fetchGitCmd(), m.fetchStateCmd(), search)
}

// aiCheckTimeLua reloads every open buffer whose file changed on disk and
// reports which ones reloaded and which kept unsaved edits instead (the
// FileChangedShell handler in file_reload_lua.go decides).
const aiCheckTimeLua = `
local before = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == '' then
    before[b] = vim.api.nvim_buf_get_changedtick(b)
  end
end
-- One buffer at a time: a plain :checktime skips buffers that are not
-- shown in a window (the other tabs), which then kept the old text.
for b in pairs(before) do pcall(vim.cmd, 'silent! checktime ' .. b) end
local out = { reloaded = {}, kept = {} }
for b, tick in pairs(before) do
  if vim.api.nvim_buf_is_valid(b) then
    local name = vim.api.nvim_buf_get_name(b)
    if vim.bo[b].modified and vim.b[b].termocode_disk_warned then
      out.kept[#out.kept + 1] = name
    elseif vim.api.nvim_buf_get_changedtick(b) ~= tick then
      out.reloaded[#out.reloaded + 1] = name
    end
  end
end
return vim.json.encode(out)
`

// aiCheckTime reloads open files that changed on disk and says so.
func (m *Model) aiCheckTime() string {
	if m.nvim == nil {
		return ""
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiCheckTimeLua, &raw); err != nil {
		return ""
	}
	var r struct {
		Reloaded []string `json:"reloaded"`
		Kept     []string `json:"kept"`
	}
	if json.Unmarshal([]byte(raw), &r) != nil {
		return ""
	}
	rel := aiRel(aiWorkspaceRoot())
	names := func(xs []string) string {
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i] = rel(x)
		}
		return strings.Join(out, ", ")
	}
	note := ""
	if len(r.Reloaded) > 0 {
		note += "\n[the editor reloaded open files that changed on disk: " + names(r.Reloaded) + "]"
	}
	if len(r.Kept) > 0 {
		note += "\n[changed on disk but open with unsaved edits; the editor kept the user's text: " + names(r.Kept) + "]"
	}
	return note
}

// aiBuf is an editor buffer of a file.
type aiBuf struct {
	ID       int    `json:"id"`
	Path     string `json:"path"`
	Modified bool   `json:"modified"`
	Listed   bool   `json:"listed"`
	Loaded   bool   `json:"loaded"`
}

const aiBuffersUnderLua = `
local path = ...
local out = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  local name = vim.api.nvim_buf_get_name(b)
  if vim.bo[b].buftype == '' and (name == path or name:sub(1, #path + 1) == path .. '/') then
    out[#out + 1] = { id = b, path = name, modified = vim.bo[b].modified,
      listed = vim.bo[b].buflisted, loaded = vim.api.nvim_buf_is_loaded(b) }
  end
end
return vim.json.encode(out)
`

// aiBuffersUnder lists the editor buffers of path or of files under it.
// It asks nvim, not m.bufs: a file the agent opened a moment ago may not
// be in the last state snapshot yet.
func (m *Model) aiBuffersUnder(path string) ([]aiBuf, error) {
	if m.nvim == nil {
		return nil, nil
	}
	var raw string
	if err := m.nvim.ExecLuaResult(aiBuffersUnderLua, &raw, path); err != nil {
		return nil, err
	}
	var bufs []aiBuf
	if raw != "" && raw != "{}" && raw != "[]" {
		if err := json.Unmarshal([]byte(raw), &bufs); err != nil {
			return nil, err
		}
	}
	return bufs, nil
}

// aiUnsaved returns the relative paths of buffers with unsaved edits.
func aiUnsaved(bufs []aiBuf, rel func(string) string) []string {
	var out []string
	for _, b := range bufs {
		if b.Loaded && b.Modified {
			out = append(out, rel(b.Path))
		}
	}
	return out
}

// aiReloadBufferLua re-reads a loaded, unmodified buffer from disk.
// 'undoreload' keeps the old text in the undo history, so the user can
// undo the agent's write with u; window views are kept.
const aiReloadBufferLua = `
local path = ...
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_get_name(b) == path and vim.bo[b].buftype == '' and vim.api.nvim_buf_is_loaded(b) then
    local views = {}
    for _, w in ipairs(vim.fn.win_findbuf(b)) do
      views[w] = vim.api.nvim_win_call(w, vim.fn.winsaveview)
    end
    vim.api.nvim_buf_call(b, function() vim.cmd('silent! keepalt edit!') end)
    for w, v in pairs(views) do
      pcall(vim.api.nvim_win_call, w, function() vim.fn.winrestview(v) end)
    end
    return 'reloaded'
  end
end
return ''
`

// aiWriteFile is write_file on Update. An open tab with unsaved edits is
// refused: overwriting would throw the user's work away, and writing the
// agent's text into the buffer would mix it with half-done edits.
func (m *Model) aiWriteFile(abs, rel, content string) (string, tea.Cmd, error) {
	fi, statErr := os.Stat(abs)
	if statErr == nil && fi.IsDir() {
		return "", nil, fmt.Errorf("%s is a folder", rel)
	}
	bufs, err := m.aiBuffersUnder(abs)
	if err != nil {
		return "", nil, err
	}
	var open bool
	for _, b := range bufs {
		if b.Path != abs || !b.Loaded {
			continue
		}
		open = true
		if b.Modified {
			return "", nil, fmt.Errorf("not written: %s is open in the editor with unsaved changes. "+
				"Ask the user to save or discard them first; do not overwrite their work", rel)
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", nil, err
	}
	out := "wrote " + rel
	if open && m.nvim != nil {
		var r string
		if err := m.nvim.ExecLuaResult(aiReloadBufferLua, &r, abs); err == nil && r != "" {
			out += "; the open editor tab shows the new content (the user can undo it)"
		}
	}
	reveal := ""
	if statErr != nil { // a new file
		reveal = abs
	}
	note, cmd := m.aiSyncIDE(reveal, aiSyncDisk)
	return out + note, cmd, nil
}

// aiDeletePath is delete on Update: refuses unsaved tabs, closes the
// tabs of the deleted files (before the delete: closing a buffer may
// auto-save it), then removes the path.
func (m *Model) aiDeletePath(abs string, rel func(string) string) (string, tea.Cmd, error) {
	if _, err := os.Lstat(abs); err != nil {
		return "", nil, fmt.Errorf("%s does not exist", rel(abs))
	}
	bufs, err := m.aiBuffersUnder(abs)
	if err != nil {
		return "", nil, err
	}
	if dirty := aiUnsaved(bufs, rel); len(dirty) > 0 {
		return "", nil, fmt.Errorf("not deleted: %s open in the editor with unsaved changes. "+
			"Ask the user to save or close it first", strings.Join(dirty, ", "))
	}
	var closed []string
	for _, b := range bufs {
		if b.Listed && b.Loaded {
			closed = append(closed, rel(b.Path))
		}
		if m.nvim != nil {
			// bdelete moves the window to another buffer first; the
			// wipe then drops the unlisted leftover of the gone file.
			_ = m.nvim.ExecLuaArgs(`local b = ... ; vim.bo[b].modified = false
				if vim.bo[b].buflisted and vim.api.nvim_buf_is_loaded(b) then pcall(vim.cmd, 'bdelete! ' .. b) end
				if vim.api.nvim_buf_is_valid(b) then pcall(vim.api.nvim_buf_delete, b, { force = true }) end`, b.ID)
		}
	}
	if err := osRemoveAll(abs); err != nil {
		return "", nil, err
	}
	m.forgetPaths([]string{abs}) // closed tabs, recents, hot-exit backups
	out := "deleted " + rel(abs) + "; it is gone from the explorer"
	if len(closed) > 0 {
		out += "; closed its editor tab(s): " + strings.Join(closed, ", ")
	}
	note, cmd := m.aiSyncIDE("", aiSyncDisk)
	return out + note, cmd, nil
}

// aiRenamePath is rename on Update. Open tabs follow the new name and
// keep unsaved edits, closed tabs / recents / hot-exit backups follow too
// (followRename); an existing target is refused
// (os.Rename would silently replace it).
func (m *Model) aiRenamePath(from, to string, rel func(string) string) (string, tea.Cmd, error) {
	fi, err := os.Lstat(from)
	if err != nil {
		return "", nil, fmt.Errorf("%s does not exist", rel(from))
	}
	if ti, err := os.Lstat(to); err == nil && !os.SameFile(fi, ti) {
		return "", nil, fmt.Errorf("%s already exists; pick another name or delete it first", rel(to))
	}
	if fi.IsDir() && strings.HasPrefix(to, from+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("cannot move %s into itself", rel(from))
	}
	bufs, err := m.aiBuffersUnder(from)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.Rename(from, to); err != nil {
		return "", nil, err
	}
	m.followRename(from, to)
	out := "renamed " + rel(from) + " to " + rel(to)
	var moved []string
	for _, b := range bufs {
		if b.Listed && b.Loaded {
			moved = append(moved, rel(to+strings.TrimPrefix(b.Path, from)))
		}
	}
	if len(moved) > 0 {
		out += "; the open editor tab(s) now show " + strings.Join(moved, ", ")
	}
	note, cmd := m.aiSyncIDE(to, aiSyncDisk)
	return out + note, cmd, nil
}
