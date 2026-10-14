package testkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// K8: the AI manages language servers and debug adapters (lsp_servers,
// dap_adapters), extensions and workspace folders. Installs use a fake
// package source (no network): it writes the shim like a real install.

// fakeInstaller swaps the real installer for one that "downloads"
// nothing: it reports two progress lines and writes the tool's shim.
// Tools named in fail return an error instead.
func fakeInstaller(d *Driver, fail ...string) {
	d.T.Helper()
	restore := app.SetToolInstaller(func(_ context.Context, t lspinstall.Tool, progress lspinstall.Progress) (string, error) {
		progress("[1/2] fake fetch " + t.Name)
		for _, f := range fail {
			if f == t.Name {
				return "fake output: no such package", errors.New("fake install failed")
			}
		}
		progress("[2/2] fake link " + t.Name)
		bin, err := lspinstall.BinDir()
		if err != nil {
			return "", err
		}
		return "", lspinstall.WriteShim(bin, t.Bin, t.Name, []string{"/bin/sh"})
	})
	d.T.Cleanup(restore)
}

// managed reports whether termocode's tools dir has its own copy of name.
func managed(name string) bool {
	t, ok := lspinstall.Lookup(name)
	return ok && lspinstall.StatusOf(t).State == lspinstall.Managed
}

// fakeShim installs name's shim directly (an earlier managed install).
func fakeShim(d *Driver, name string) {
	d.T.Helper()
	t, _ := lspinstall.Lookup(name)
	bin, err := lspinstall.BinDir()
	if err == nil {
		err = lspinstall.WriteShim(bin, t.Bin, t.Name, []string{"/bin/sh"})
	}
	if err != nil {
		d.T.Fatal(err)
	}
}

// K8 rows of TestAIToolsTable.
var aiAdminToolCases = []aiToolCase{
	{
		name:  "lsp_servers list shows the workspace languages and their servers",
		call:  toolCall{"lsp_servers", map[string]any{"op": "list"}},
		setup: func(d *Driver) { writeFiles(d, map[string]string{"main.go": "package main\n", "tool.py": "x = 1\n"}) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 {
				t.Fatalf("results %q", r)
			}
			for _, want := range []string{"Workspace languages: go, python", "gopls (Go)", "pyright (Python)", "lsp_servers(op: \"install\""} {
				if !strings.Contains(r[0], want) {
					t.Errorf("list has no %q:\n%s", want, r[0])
				}
			}
			if strings.Contains(r[0], "rust_analyzer") {
				t.Errorf("list shows a server for a language the workspace does not use:\n%s", r[0])
			}
		},
	},
	{
		name:    "lsp_servers install asks, installs in the background like the UI and waits",
		call:    toolCall{"lsp_servers", map[string]any{"op": "install", "name": "gopls"}},
		setup:   func(d *Driver) { fakeInstaller(d) },
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "installed gopls") {
				t.Errorf("results %q", r)
			}
			if !managed("gopls") {
				t.Error("gopls is not installed in the tools dir")
			}
			if !eventually(d, func() bool { return strings.Contains(screen(d), "gopls installed") }) {
				t.Errorf("no install toast:\n%s", screen(d))
			}
		},
	},
	{
		name:    "lsp_servers install failure goes back to the model with the output",
		call:    toolCall{"lsp_servers", map[string]any{"op": "install", "name": "pyright"}},
		setup:   func(d *Driver) { fakeInstaller(d, "pyright") },
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "fake install failed") || !strings.Contains(r[0], "no such package") {
				t.Errorf("results %q", r)
			}
			if managed("pyright") {
				t.Error("a failed install left pyright installed")
			}
		},
	},
	{
		name:  "lsp_servers install of an unknown server is refused without a dialog",
		call:  toolCall{"lsp_servers", map[string]any{"op": "install", "name": "nope"}},
		setup: func(d *Driver) { fakeInstaller(d) },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "no language server") || !strings.Contains(r[0], "gopls") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name:    "lsp_servers uninstall always asks and removes termocode's copy",
		call:    toolCall{"lsp_servers", map[string]any{"op": "uninstall", "name": "gopls"}},
		setup:   func(d *Driver) { fakeShim(d, "gopls") },
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "removed gopls") {
				t.Errorf("results %q", r)
			}
			if managed("gopls") {
				t.Error("gopls shim still there")
			}
		},
	},
	{
		name:  "dap_adapters list shows the debug adapters for the workspace",
		call:  toolCall{"dap_adapters", map[string]any{"op": "list"}},
		setup: func(d *Driver) { writeFiles(d, map[string]string{"main.go": "package main\n"}); fakeShim(d, "dlv") },
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "dlv (Go): installed by termocode") {
				t.Errorf("results %q", r)
			}
		},
	},
	{
		name: "workspace_folders open is refused: it would restart termocode",
		call: toolCall{"workspace_folders", map[string]any{"op": "open", "path": "/tmp"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "restarts termocode") {
				t.Errorf("results %q", r)
			}
			if d.Quit() {
				t.Error("the app quit")
			}
		},
	},
}

func init() { aiToolCases = append(aiToolCases, aiAdminToolCases...) }

// allowNext waits for the "AI wants to" dialog that shows want and
// clicks Allow.
func allowNext(d *Driver, want string) {
	d.T.Helper()
	if !d.WaitFor(func() bool {
		s := ansi.Strip(d.render())
		return strings.Contains(s, "AI wants to") && strings.Contains(s, want)
	}, 5*time.Second) {
		d.T.Fatalf("no confirm dialog for %q:\n%s", want, screen(d))
	}
	clickAIAllow(d)
}

// Spec: the AI adds a folder to the workspace (it shows as a new root in
// the explorer), lists the roots and removes it again; both changes ask.
func TestAIWorkspaceFoldersTool(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "k8extra")
	if err := os.MkdirAll(filepath.Join(extra, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, srv := fakeToolModel(t,
		toolCall{"workspace_folders", map[string]any{"op": "add", "path": extra}},
		toolCall{"workspace_folders", map[string]any{"op": "list"}},
		toolCall{"workspace_folders", map[string]any{"op": "remove", "path": "k8extra"}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "add it")
	allowNext(d, "Add folder")
	if !d.WaitFor(func() bool { return explorerShows(d, "k8extra") }, 5*time.Second) {
		t.Fatalf("the new root is not in the explorer:\n%s", screen(d))
	}
	allowNext(d, "Remove folder")
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 3 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], "added "+extra) {
		t.Errorf("add: %q", r[0])
	}
	if !strings.Contains(r[1], "(primary") || !strings.Contains(r[1], extra) {
		t.Errorf("list: %q", r[1])
	}
	if !strings.Contains(r[2], "removed "+extra) {
		t.Errorf("remove: %q", r[2])
	}
	if !eventually(d, func() bool { return !explorerShows(d, "k8extra") }) {
		t.Errorf("the removed root is still in the explorer:\n%s", screen(d))
	}
}

// k8Extension registers one command that logs and shows a toast.
const k8Extension = `termocode.register_command{ id = 'hello', title = 'K8 Hello', run = function()
  termocode.log('k8-ran')
  termocode.notify('k8 hello toast')
end }
`

// Spec: the AI reloads the extensions (a new one in the folder loads),
// lists them with their commands, runs a command and reads the log.
func TestAIExtensionsTool(t *testing.T) {
	s, srv := fakeToolModel(t,
		toolCall{"extensions", map[string]any{"op": "reload"}},
		toolCall{"extensions", map[string]any{"op": "list"}},
		toolCall{"extensions", map[string]any{"op": "run", "id": "k8demo.hello"}},
		toolCall{"extensions", map[string]any{"op": "log"}},
	)
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "extensions", "k8demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(k8Extension), 0o644); err != nil {
		t.Fatal(err)
	}
	askAI(d, "reload and run it")
	allowNext(d, "Reload extensions")
	allowNext(d, "Run extension command")
	if !d.WaitForText("fake-done", 10*time.Second) {
		t.Fatalf("no final answer:\n%s", screen(d))
	}
	r := s.toolResults()
	if len(r) != 4 {
		t.Fatalf("results %q", r)
	}
	if !strings.Contains(r[0], "k8demo") || !strings.Contains(r[0], "k8demo.hello") {
		t.Errorf("reload: %q", r[0])
	}
	if !strings.Contains(r[1], "k8demo: loaded") || !strings.Contains(r[1], "k8demo.hello: K8 Hello") {
		t.Errorf("list: %q", r[1])
	}
	if !strings.Contains(r[2], "ran k8demo.hello") || !strings.Contains(r[2], "k8-ran") {
		t.Errorf("run: %q", r[2])
	}
	if !strings.Contains(r[3], "k8-ran") {
		t.Errorf("log: %q", r[3])
	}
	if !eventually(d, func() bool { return strings.Contains(screen(d), "k8 hello toast") }) {
		t.Errorf("the command's toast is not on screen:\n%s", screen(d))
	}
}
