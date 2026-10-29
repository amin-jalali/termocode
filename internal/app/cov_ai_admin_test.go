package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/explorer"
	extpkg "github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// K8 admin tools (ai_admin_tools.go): installs through the installer
// seam (no network), extensions, and workspace folders.

// covAsyncCall runs fn on the model and feeds the messages of the Cmds
// it returns back through route (like the Bubble Tea loop would).
func covAsyncCall(t *testing.T, m *Model, route func(*Model, tea.Msg) (tea.Cmd, bool)) aiIDECaller {
	var mu sync.Mutex
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			select {
			case <-done:
				return
			default:
			}
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, x := range b {
					run(x)
				}
				return
			}
			mu.Lock()
			next, ok := route(m, msg)
			mu.Unlock()
			if ok {
				run(next)
			}
		}()
	}
	return func(ctx context.Context, fn aiIDEFunc) (string, error) {
		mu.Lock()
		out, cmd, err := fn(m)
		mu.Unlock()
		run(cmd)
		return out, err
	}
}

func covLSPRoute(m *Model, msg tea.Msg) (tea.Cmd, bool) {
	if ev, ok := msg.(lspInstallEventMsg); ok {
		return m.onToolInstallEvent(ev), true
	}
	return nil, false
}

func covAdminTool(t *testing.T, root string, call aiIDECaller, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiAdminTools(root, call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// Install through the seam: progress, a good end, a failing one with its
// output, and a slow one that keeps going in the background.
func TestCovAIInstallFlows(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644)
	m := &Model{w: 100, h: 30}
	call := covAsyncCall(t, m, covLSPRoute)
	release := make(chan struct{})
	var mode string
	restore := SetToolInstaller(func(ctx context.Context, tl lspinstall.Tool, p lspinstall.Progress) (string, error) {
		p("downloading " + tl.Name)
		switch mode {
		case "fail":
			return "line 1\nline 2\nfatal: no space", &lspinstall.MissingToolchainError{Bin: "go", Hint: "install Go"}
		case "offline":
			return "", &lspinstall.OfflineError{Tool: tl.Name, Err: errors.New("dial: no route")}
		case "slow":
			<-release
		}
		return "ok", nil
	})
	t.Cleanup(restore)
	t.Cleanup(func() { close(release) })
	lsp := covAdminTool(t, root, call, "lsp_servers")
	out, err := lsp.Run(context.Background(), map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, "Workspace languages: go") || !strings.Contains(out, "gopls") {
		t.Errorf("list: %q %v", out, err)
	}
	out, err = lsp.Run(context.Background(), map[string]any{"op": "install", "name": "gopls", "wait_ms": 10000})
	if err != nil || !strings.HasPrefix(out, "installed gopls (") {
		t.Errorf("install: %q %v", out, err)
	}
	mode = "fail"
	_, err = lsp.Run(context.Background(), map[string]any{"op": "install", "name": "gopls", "wait_ms": 10000})
	if err == nil || !strings.Contains(err.Error(), "go is not on PATH (install Go)") || !strings.Contains(err.Error(), "Last output:\nline 1") {
		t.Errorf("failed install: %v", err)
	}
	out, _ = lsp.Run(context.Background(), map[string]any{"op": "list"})
	if !strings.Contains(out, "last install failed: go is not on PATH") {
		t.Errorf("list after a failure: %q", out)
	}
	mode = "offline"
	if _, err := lsp.Run(context.Background(), map[string]any{"op": "install", "name": "gopls"}); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Errorf("offline: %v", err)
	}
	mode = "slow"
	out, err = lsp.Run(context.Background(), map[string]any{"op": "install", "name": "gopls", "wait_ms": 50})
	if err != nil || !strings.HasPrefix(out, "installing gopls in the background") {
		t.Errorf("slow: %q %v", out, err)
	}
	out, _ = lsp.Run(context.Background(), map[string]any{"op": "list", "all": true})
	if !strings.Contains(out, "gopls (Go): installing… (downloading gopls)") {
		t.Errorf("list while installing: %q", out)
	}
	if _, err := lsp.Run(context.Background(), map[string]any{"op": "uninstall", "name": "gopls"}); err == nil || !strings.Contains(err.Error(), "installing right now") {
		t.Errorf("uninstall while installing: %v", err)
	}
	if err := lsp.Validate(map[string]any{"op": "uninstall", "name": "gopls"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("uninstall of a missing tool: %v", err)
	}
	for op, want := range map[string]string{"install": "Install language server gopls (downloads and runs its installer)", "uninstall": "Uninstall language server gopls", "list": "List language servers"} {
		if got := lsp.Describe(map[string]any{"op": op, "name": "gopls"}); got != want {
			t.Errorf("%s card = %q", op, got)
		}
	}
	if _, err := lsp.Run(context.Background(), map[string]any{"op": "install", "name": "nope"}); err == nil {
		t.Error("unknown name ran")
	}
}

// A list with no tool for the workspace's languages says how to see all.
func TestCovAIInstallListEmptyWorkspace(t *testing.T) {
	root := t.TempDir()
	m := &Model{}
	dap := covAdminTool(t, root, covSyncCall(m), "dap_adapters")
	out, err := dap.Run(context.Background(), map[string]any{"op": "list"})
	if err != nil || !strings.Contains(out, "Workspace languages: none found") || !strings.Contains(out, "all: true lists every one") {
		t.Errorf("empty list: %q %v", out, err)
	}
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if _, err := covAdminTool(t, root, stopped, "dap_adapters").Run(context.Background(), map[string]any{"op": "list"}); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
	if aiInstallTail(strings.Repeat("x\n", aiInstallOutLines+5)) != strings.TrimSpace(strings.Repeat("x\n", aiInstallOutLines)) {
		t.Error("tail is not cut")
	}
	if aiInstallErrText(errors.New("plain")) != "plain" {
		t.Error("plain error text")
	}
}

// Extensions without the editor; a registry with commands, panels,
// status items, a failed one and a problem; and runs against nvim.
func TestCovAIExtensions(t *testing.T) {
	m := &Model{}
	call := covSyncCall(m)
	tl := aiExtensionsTool(call)
	out, err := tl.Run(context.Background(), map[string]any{"op": "list"})
	if err != nil || !strings.HasSuffix(out, "Extensions are not available (no editor).") {
		t.Errorf("no editor: %q %v", out, err)
	}
	for _, op := range []string{"run", "reload"} {
		if _, err := tl.Run(context.Background(), map[string]any{"op": op, "id": "x"}); err == nil || !strings.Contains(err.Error(), "not available") {
			t.Errorf("%s without the editor: %v", op, err)
		}
	}
	if out, err := tl.Run(context.Background(), map[string]any{"op": "log"}); err != nil || !strings.Contains(out, "not available") {
		t.Errorf("log without the editor: %q %v", out, err)
	}
	m.ext = &extState{loading: true, loadErr: "syntax error in todo.lua",
		reg: extpkg.Registry{
			Extensions: []extpkg.Loaded{{Name: "todo", Version: "1.2", OK: true}, {Name: "bad", Error: "boom"}},
			Commands:   []extpkg.Command{{ID: "todo.refresh", Title: "Refresh TODOs"}},
			Panels:     []extpkg.Panel{{ID: "todo.panel", Title: "TODO", Ext: "todo"}},
			Status:     []extpkg.StatusItem{{ID: "todo.count", Ext: "todo"}},
		},
		exts: []extpkg.Extension{{Name: "broken", Problem: "no init.lua"}}}
	out = m.aiExtList()
	m.ext.loading = false
	if !strings.Contains(m.aiExtList(), "Load failed: syntax error in todo.lua") {
		t.Error("the load error is not listed")
	}
	covAIMustContain(t, "ext list", out, "(loading…)", "- todo v1.2: loaded", "- bad: failed: boom",
		"- broken: problem: no init.lua", "- todo.refresh: Refresh TODOs", "Panel todo.panel: TODO (todo)", "Status item todo.count (todo)")
	for op, want := range map[string]string{"run": "Run extension command x", "reload": "Reload extensions (runs their code)", "log": "Read the extension log", "list": "List extensions"} {
		if got := tl.Describe(map[string]any{"op": op, "id": "x"}); got != want {
			t.Errorf("%s card = %q", op, got)
		}
	}
	if tl.Validate(map[string]any{"op": "run"}) == nil || tl.Validate(map[string]any{"op": "jump"}) == nil {
		t.Error("ext validate")
	}

	c := covNvim(t)
	m.nvim = c
	if _, err := tl.Run(context.Background(), map[string]any{"op": "run", "id": "todo.refresh"}); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Errorf("run before load: %v", err)
	}
	m.ext.loaded, m.ext.loading, m.ext.loadErr = true, false, ""
	if _, err := tl.Run(context.Background(), map[string]any{"op": "run", "id": "nope"}); err == nil || !strings.Contains(err.Error(), `no extension command "nope"`) {
		t.Errorf("unknown id: %v", err)
	}
	if err := c.ExecLua(`_G._termocode_ext = { log_lines = {}, log_text = function() return 'one\ntwo\nthree\n' end,
		run = function(id)
			local H = _G._termocode_ext
			if id == 'todo.fail' then H.log_lines[#H.log_lines + 1] = 'ERROR todo.fail: boom'
			else H.log_lines[#H.log_lines + 1] = 'refreshed 3 files' end
		end }`); err != nil {
		t.Fatal(err)
	}
	out, err = tl.Run(context.Background(), map[string]any{"op": "run", "id": "todo.refresh"})
	if err != nil || out != "ran todo.refresh (Refresh TODOs); it logged:\nrefreshed 3 files" {
		t.Errorf("run: %q %v", out, err)
	}
	m.ext.reg.Commands = append(m.ext.reg.Commands, extpkg.Command{ID: "todo.fail", Title: "Fail"})
	if _, err := tl.Run(context.Background(), map[string]any{"op": "run", "id": "todo.fail"}); err == nil || !strings.Contains(err.Error(), "ERROR todo.fail: boom") {
		t.Errorf("failing run: %v", err)
	}
	out, err = tl.Run(context.Background(), map[string]any{"op": "log", "lines": 2})
	if err != nil || !strings.HasSuffix(out, "Log:\ntwo\nthree") {
		t.Errorf("log: %q %v", out, err)
	}
	_ = c.ExecLua(`_G._termocode_ext.log_text = function() return '' end`)
	if out, _ := tl.Run(context.Background(), map[string]any{"op": "log"}); !strings.HasSuffix(out, "Log: empty") {
		t.Errorf("empty log: %q", out)
	}
	_ = c.ExecLua(`_G._termocode_ext.log_text = function() error('broken log') end`)
	if out, _ := tl.Run(context.Background(), map[string]any{"op": "log"}); !strings.Contains(out, "(log not available: ") {
		t.Errorf("broken log: %q", out)
	}
}

// Workspace folders: add one, list, add it again, remove by name, and
// the refusals (primary, unknown, a file, open).
func TestCovAIWorkspaceFolders(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	extra := filepath.Join(t.TempDir(), "lib")
	_ = os.MkdirAll(extra, 0o755)
	file := filepath.Join(root, "f.txt")
	_ = os.WriteFile(file, nil, 0o644)
	m := &Model{w: 100, h: 30}
	m.explorer = explorer.New()
	tl := aiWorkspaceFoldersTool(root, covSyncCall(m))
	ctx := context.Background()
	for _, a := range []map[string]any{{"op": "add", "path": file}, {"op": "add", "path": filepath.Join(root, "missing")}, {"op": "remove"}} {
		if tl.Validate(a) == nil {
			t.Errorf("%v accepted", a)
		}
	}
	if _, err := tl.Run(ctx, map[string]any{"op": "open", "path": extra}); err == nil {
		t.Error("open ran")
	}
	out, err := tl.Run(ctx, map[string]any{"op": "add", "path": extra})
	if err != nil || !strings.HasPrefix(out, "added "+extra) {
		t.Fatalf("add: %q %v", out, err)
	}
	if out, _ := tl.Run(ctx, map[string]any{"op": "add", "path": extra}); out != extra+" is already in the workspace" {
		t.Errorf("add again: %q", out)
	}
	out, _ = tl.Run(ctx, map[string]any{"op": "list"})
	if !strings.HasPrefix(out, "2 workspace folders:") || !strings.Contains(out, "(primary; ") || !strings.Contains(out, "- "+extra) {
		t.Errorf("list: %q", out)
	}
	if _, err := tl.Run(ctx, map[string]any{"op": "remove", "path": "nope"}); err == nil || !strings.Contains(err.Error(), "is not a workspace folder") {
		t.Errorf("remove unknown: %v", err)
	}
	if _, err := tl.Run(ctx, map[string]any{"op": "remove", "path": m.explorer.PrimaryRoot()}); err == nil || !strings.Contains(err.Error(), "primary folder") {
		t.Errorf("remove primary: %v", err)
	}
	if out, err := tl.Run(ctx, map[string]any{"op": "remove", "path": "lib"}); err != nil || !strings.HasPrefix(out, "removed "+extra) {
		t.Errorf("remove by name: %q %v", out, err)
	}
	for op, want := range map[string]string{"add": "Add folder to the workspace: p", "remove": "Remove folder from the workspace: p", "list": "List workspace folders"} {
		if got := tl.Describe(map[string]any{"op": op, "path": "p"}); got != want {
			t.Errorf("%s card = %q", op, got)
		}
	}
	// Two added folders with the same name: remove asks for the path.
	a, b := filepath.Join(t.TempDir(), "same"), filepath.Join(t.TempDir(), "same")
	_ = os.MkdirAll(a, 0o755)
	_ = os.MkdirAll(b, 0o755)
	_, _ = tl.Run(ctx, map[string]any{"op": "add", "path": a})
	_, _ = tl.Run(ctx, map[string]any{"op": "add", "path": b})
	if _, err := tl.Run(ctx, map[string]any{"op": "remove", "path": "same"}); err == nil || !strings.Contains(err.Error(), "more than one workspace folder") {
		t.Errorf("ambiguous name: %v", err)
	}
}
