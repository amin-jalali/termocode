package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// The IDE follows each file / git change the agent makes. Each test runs
// one agent turn with the scripted provider (tools auto-approved) on a
// real nvim and checks the editor buffers, the disk, the explorer and
// what the model was told.

// aiSyncModel is aiIDETestModel with nvim, an explorer on the workspace,
// the files written and every tool class auto-approved.
func aiSyncModel(t *testing.T, p ai.Provider, files map[string]string) (*Model, *nvim.Client, string) {
	t.Helper()
	m, dir := aiIDETestModel(t, p)
	for rel, body := range files {
		abs := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := aiTestNvim(t, m)
	if err := c.ExecLua(fileReloadLua); err != nil { // as the app does at startup
		t.Fatal(err)
	}
	m.explorer = explorer.New()
	m.ai.settings.AutoApprove = ai.ToolClasses
	return m, c, dir
}

func bufText(t *testing.T, c *nvim.Client, path string) string {
	t.Helper()
	var s string
	if err := c.ExecLuaResult(`local p = ...
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_get_name(b) == p and vim.api.nvim_buf_is_loaded(b) then
				return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')
			end
		end
		return '<no buffer>'`, &s, path); err != nil {
		t.Fatal(err)
	}
	return s
}

func bufNames(t *testing.T, c *nvim.Client) string {
	t.Helper()
	s, err := c.EvalLuaString(`local t = {}
		for _, b in ipairs(vim.api.nvim_list_bufs()) do t[#t + 1] = vim.api.nvim_buf_get_name(b) end
		return table.concat(t, '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func bufModified(t *testing.T, c *nvim.Client, path string) bool {
	t.Helper()
	var s string
	_ = c.ExecLuaResult(`local p = ...
		for _, b in ipairs(vim.api.nvim_list_bufs()) do
			if vim.api.nvim_buf_get_name(b) == p then return tostring(vim.bo[b].modified) end
		end
		return 'none'`, &s, path)
	return s == "true"
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func oneResult(t *testing.T, p *scriptedAIProvider) string {
	t.Helper()
	r := p.toolResults()
	if len(r) != 1 {
		t.Fatalf("tool results %v", r)
	}
	return r[0]
}

// Spec: write_file on a file open in the editor (no unsaved edits): the
// tab shows the new text, the disk has exactly it, u undoes it, and the
// model is told the tab shows it.
func TestAIWriteFileUpdatesOpenBuffer(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": "new one\nnew two\n"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "old\n"})
	path := filepath.Join(dir, "a.txt")
	m.openFileAtLine(path, 1)
	driveAIChat(t, m, m.sendAIChat("rewrite a"), nil)
	if r := oneResult(t, p); strings.Contains(r, "error") || !strings.Contains(r, "open editor tab shows the new content") {
		t.Fatalf("result %q", r)
	}
	if got := readFile(path); got != "new one\nnew two\n" {
		t.Errorf("disk %q", got)
	}
	if got := bufText(t, c, path); got != "new one\nnew two" {
		t.Errorf("buffer %q", got)
	}
	if bufModified(t, c, path) {
		t.Error("buffer marked modified after the write")
	}
	_ = c.Command("normal! u")
	if got := bufText(t, c, path); got != "old" {
		t.Errorf("undo: buffer %q, want the old text", got)
	}
}

// Spec: write_file on a tab with unsaved edits is refused; the user's
// edits and the disk stay as they were.
func TestAIWriteFileRefusesUnsavedBuffer(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": "agent\n"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "old\n"})
	path := filepath.Join(dir, "a.txt")
	m.openFileAtLine(path, 1)
	_ = c.Command("normal! Imine ")
	driveAIChat(t, m, m.sendAIChat("rewrite a"), nil)
	if r := oneResult(t, p); !strings.Contains(r, "error") || !strings.Contains(r, "unsaved changes") {
		t.Fatalf("result %q, want a refusal", r)
	}
	if got := readFile(path); got != "old\n" {
		t.Errorf("disk %q changed", got)
	}
	if got := bufText(t, c, path); got != "mine old" || !bufModified(t, c, path) {
		t.Errorf("user edits lost: buffer %q", got)
	}
}

// Spec: create_file / write_file of a new file / create_dir reveal the new
// entry in the explorer and tell the model so.
func TestAICreateRevealsInExplorer(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
		path string
	}{
		{"create_file", map[string]any{"path": "sub/new.md", "content": "x\n"}, "sub/new.md"},
		{"write_file", map[string]any{"path": "deep/w.md", "content": "x\n"}, "deep/w.md"},
		{"create_dir", map[string]any{"path": "pkg/inner"}, "pkg/inner"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			p := &scriptedAIProvider{replies: []ai.Chunk{toolCall(tc.tool, tc.args)}}
			m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n"})
			driveAIChat(t, m, m.sendAIChat("make it"), nil)
			if r := oneResult(t, p); strings.Contains(r, "error") || !strings.Contains(r, "shown in the explorer") {
				t.Fatalf("result %q", r)
			}
			if got, _ := m.ExplorerCursor(); got != filepath.Join(dir, tc.path) {
				t.Errorf("explorer cursor %q, want the new %s", got, tc.path)
			}
		})
	}
}

// Spec: a new file the explorer hides (a dotfile) is not reported as
// shown in the explorer.
func TestAICreateHiddenFileSaysNotListed(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("create_file", map[string]any{"path": ".env.local"})}}
	m, _, _ := aiSyncModel(t, p, map[string]string{"a.txt": "a\n"})
	driveAIChat(t, m, m.sendAIChat("make it"), nil)
	r := oneResult(t, p)
	if m.explorer.HiddenShown() {
		t.Skip("explorer shows hidden files by default")
	}
	if strings.Contains(r, "shown in the explorer") || !strings.Contains(r, "hides it") {
		t.Errorf("result %q", r)
	}
}

// Spec: delete of an open file closes its tab (no ghost editor) and of a
// folder closes the tabs of the files inside.
func TestAIDeleteClosesOpenTabs(t *testing.T) {
	for _, target := range []string{"b.txt", "dir"} {
		t.Run(target, func(t *testing.T) {
			p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("delete", map[string]any{"path": target})}}
			m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "b.txt": "b\n", "dir/x.txt": "x\n", "dir/y.txt": "y\n"})
			m.openFileAtLine(filepath.Join(dir, "a.txt"), 1)
			m.openFileAtLine(filepath.Join(dir, "b.txt"), 1)
			m.openFileAtLine(filepath.Join(dir, "dir/x.txt"), 1)
			m.openFileAtLine(filepath.Join(dir, "dir/y.txt"), 1)
			driveAIChat(t, m, m.sendAIChat("delete it"), nil)
			r := oneResult(t, p)
			if strings.Contains(r, "error") || !strings.Contains(r, "closed its editor tab") {
				t.Fatalf("result %q", r)
			}
			if _, err := os.Stat(filepath.Join(dir, target)); err == nil {
				t.Errorf("%s still on disk", target)
			}
			names := bufNames(t, c)
			if strings.Contains(names, "/"+target) {
				t.Errorf("a buffer of the deleted %s is still open:\n%s", target, names)
			}
			if !strings.Contains(names, "/a.txt") {
				t.Errorf("a.txt was closed too:\n%s", names)
			}
		})
	}
}

// Spec: delete of a file (or folder) with an unsaved tab is refused.
func TestAIDeleteRefusesUnsavedTab(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("delete", map[string]any{"path": "dir"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"dir/x.txt": "x\n"})
	path := filepath.Join(dir, "dir/x.txt")
	m.openFileAtLine(path, 1)
	_ = c.Command("normal! Imine ")
	driveAIChat(t, m, m.sendAIChat("delete it"), nil)
	if r := oneResult(t, p); !strings.Contains(r, "error") || !strings.Contains(r, "unsaved changes") || !strings.Contains(r, "dir/x.txt") {
		t.Fatalf("result %q, want a refusal naming dir/x.txt", r)
	}
	if readFile(path) != "x\n" || bufText(t, c, path) != "mine x" {
		t.Errorf("file or edits lost: disk %q buffer %q", readFile(path), bufText(t, c, path))
	}
}

// Spec: rename of an open file with unsaved edits: the tab follows the
// new name, keeps the edits (still unsaved), no buffer of the old name is
// left, and saving writes the new file only.
func TestAIRenameOpenFileKeepsEdits(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("rename", map[string]any{"from": "test.md", "to": "test_001.md"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"test.md": "hello\n"})
	oldP, newP := filepath.Join(dir, "test.md"), filepath.Join(dir, "test_001.md")
	m.openFileAtLine(oldP, 1)
	_ = c.Command("normal! Amore")
	driveAIChat(t, m, m.sendAIChat("rename it"), nil)
	r := oneResult(t, p)
	if strings.Contains(r, "error") || !strings.Contains(r, "now show test_001.md") {
		t.Fatalf("result %q", r)
	}
	name, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if name != newP {
		t.Errorf("current buffer %q, want %q", name, newP)
	}
	if got := bufText(t, c, newP); got != "hellomore" || !bufModified(t, c, newP) {
		t.Errorf("edits lost: %q modified=%v", got, bufModified(t, c, newP))
	}
	if names := bufNames(t, c); strings.Contains(names, "/test.md") {
		t.Errorf("old buffer left:\n%s", names)
	}
	if got, _ := m.ExplorerCursor(); got != newP {
		t.Errorf("explorer cursor %q, want the renamed file", got)
	}
	_ = c.Command("write")
	if _, err := os.Stat(oldP); err == nil || readFile(newP) != "hellomore\n" {
		t.Errorf("save: old exists=%v new=%q", err == nil, readFile(newP))
	}
}

// Spec: rename of a folder moves the tabs of the open files inside.
func TestAIRenameFolderMovesTabs(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("rename", map[string]any{"from": "old", "to": "new"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"old/x.txt": "x\n"})
	m.openFileAtLine(filepath.Join(dir, "old/x.txt"), 1)
	driveAIChat(t, m, m.sendAIChat("rename it"), nil)
	if r := oneResult(t, p); !strings.Contains(r, "now show new/x.txt") {
		t.Fatalf("result %q", r)
	}
	names := bufNames(t, c)
	if strings.Contains(names, "/old/") || !strings.Contains(names, filepath.Join(dir, "new/x.txt")) {
		t.Errorf("buffers:\n%s", names)
	}
}

// Spec: rename onto an existing file is refused (it used to replace it).
func TestAIRenameRefusesExistingTarget(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("rename", map[string]any{"from": "a.txt", "to": "b.txt"})}}
	m, _, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	driveAIChat(t, m, m.sendAIChat("rename it"), nil)
	if r := oneResult(t, p); !strings.Contains(r, "already exists") {
		t.Fatalf("result %q", r)
	}
	if readFile(filepath.Join(dir, "b.txt")) != "b\n" || readFile(filepath.Join(dir, "a.txt")) != "a\n" {
		t.Error("files changed")
	}
}

// Spec: after run_command, open files changed by the command show the
// new text, and the model is told which ones reloaded; a file with
// unsaved edits keeps them and the model is told.
func TestAIRunCommandReloadsOpenBuffers(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("run_command", map[string]any{
		"command": "printf 'from shell\\n' > a.txt; printf 'shell b\\n' > b.txt; touch made.txt"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	m.openFileAtLine(b, 1)
	_ = c.Command("normal! Imine ")
	m.openFileAtLine(a, 1)
	driveAIChat(t, m, m.sendAIChat("run it"), nil)
	r := oneResult(t, p)
	if got := bufText(t, c, a); got != "from shell" {
		t.Errorf("a.txt buffer %q, want the new text", got)
	}
	if got := bufText(t, c, b); got != "mine b" {
		t.Errorf("b.txt buffer %q, want the user's edits kept", got)
	}
	if !strings.Contains(r, "reloaded open files that changed on disk: a.txt") || !strings.Contains(r, "kept the user's text: b.txt") {
		t.Errorf("result does not tell the model: %q", r)
	}
	m.explorer.RevealPath(filepath.Join(dir, "made.txt"))
	if got, _ := m.ExplorerCursor(); got != filepath.Join(dir, "made.txt") {
		t.Errorf("explorer does not list made.txt (cursor %q)", got)
	}
}

// Spec: the agent stops at ai_max_steps, the chat says so and offers to
// continue; on "continue" the model sees the tool steps it already did
// (they used to be left out of the history, so it started over).
func TestAIStepLimitThenContinueKeepsToolSteps(t *testing.T) {
	loop := toolCall("list_dir", map[string]any{"path": "."})
	p := &scriptedAIProvider{replies: []ai.Chunk{loop, loop, {Delta: "went on"}}}
	m, _, _ := aiSyncModel(t, p, map[string]string{"a.txt": "a\n"})
	m.ai.settings.MaxSteps = 2
	driveAIChat(t, m, m.sendAIChat("list a lot"), nil)
	if p.calls != 2 {
		t.Fatalf("provider called %d times, want the limit 2", p.calls)
	}
	msgs := m.ai.conv.Messages
	last := msgs[len(msgs)-1]
	if last.Role != ai.StoredAssistant || !strings.Contains(last.Text, "step limit (2 tool steps)") || !strings.Contains(last.Text, `"continue"`) {
		t.Fatalf("last chat message %+v", last)
	}
	driveAIChat(t, m, m.sendAIChat("continue"), nil)
	p.mu.Lock()
	hist := p.seen[len(p.seen)-1]
	p.mu.Unlock()
	var asst string
	for _, msg := range hist {
		if msg.Role == ai.RoleAssistant {
			asst += msg.Content
		}
	}
	if !strings.Contains(asst, "List .") || !strings.Contains(asst, "a.txt") {
		t.Errorf("history does not carry the earlier tool steps:\n%s", asst)
	}
	if hist[len(hist)-1].Role != ai.RoleUser || hist[len(hist)-1].Content != "continue" {
		t.Errorf("history does not end with the user's continue: %+v", hist[len(hist)-1])
	}
}

// Spec: git_checkout reloads open files that differ on the other branch,
// also a tab in the background (a plain :checktime skips those).
func TestAIGitCheckoutReloadsOpenBuffers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("git_checkout", map[string]any{"branch": "other"})}}
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "main\n", "c.txt": "c\n"})
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitRun("init", "-q", "-b", "main")
	gitRun("add", ".")
	gitRun("commit", "-qm", "one")
	gitRun("checkout", "-qb", "other")
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("other\n"), 0o644)
	gitRun("commit", "-qam", "two")
	gitRun("checkout", "-q", "main")
	a := filepath.Join(dir, "a.txt")
	m.openFileAtLine(a, 1)
	m.openFileAtLine(filepath.Join(dir, "c.txt"), 1) // a.txt is now a tab in the background
	driveAIChat(t, m, m.sendAIChat("switch"), nil)
	if got := bufText(t, c, a); got != "other" {
		t.Errorf("buffer %q after checkout, want the other branch's text (result %q)", got, oneResult(t, p))
	}
}
