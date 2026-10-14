package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// K8: the workspace languages come from the file names, without the
// folders nobody edits (node_modules, hidden folders).
func TestAIWorkspaceFiletypes(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"main.go", "go.mod", "tools/x.py", "node_modules/pkg/i.ts", ".hidden/y.rs", "README.md"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := aiWorkspaceFiletypes(dir)
	want := []string{"go", "gomod", "python"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filetypes %v, want %v", got, want)
	}
}

func aiAdminTool(t *testing.T, name string) ai.Tool {
	t.Helper()
	tl, ok := ai.FindTool(aiAdminTools(t.TempDir(), nil), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// K8: every op has its own gate: reading is auto, installing and
// running extension code ask with class run, uninstall always asks,
// workspace folders ask with class ide.
func TestAIAdminToolGates(t *testing.T) {
	type gate struct {
		mut   bool
		class string
	}
	cases := []struct {
		tool, op string
		want     gate
	}{
		{"lsp_servers", "list", gate{false, ""}},
		{"lsp_servers", "install", gate{true, ai.ClassRun}},
		{"lsp_servers", "uninstall", gate{true, ""}},
		{"dap_adapters", "list", gate{false, ""}},
		{"dap_adapters", "install", gate{true, ai.ClassRun}},
		{"extensions", "list", gate{false, ""}},
		{"extensions", "log", gate{false, ""}},
		{"extensions", "run", gate{true, ai.ClassRun}},
		{"extensions", "reload", gate{true, ai.ClassRun}},
		{"workspace_folders", "list", gate{false, ""}},
		{"workspace_folders", "add", gate{true, ai.ClassIDE}},
		{"workspace_folders", "remove", gate{true, ai.ClassIDE}},
	}
	for _, c := range cases {
		tl := aiAdminTool(t, c.tool)
		mut, class := tl.Gate(map[string]any{"op": c.op})
		if (gate{mut, class}) != c.want {
			t.Errorf("%s %s: gate (%v, %q), want %+v", c.tool, c.op, mut, class, c.want)
		}
	}
}

// K8: bad calls are refused before the confirm dialog, with a reason.
func TestAIAdminToolValidate(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want string // "" = valid
	}{
		{"lsp_servers", map[string]any{"op": "list"}, ""},
		{"lsp_servers", map[string]any{"op": "fly"}, "op must be one of"},
		{"lsp_servers", map[string]any{"op": "install"}, "needs name"},
		{"lsp_servers", map[string]any{"op": "install", "name": "nope"}, "no language server"},
		{"lsp_servers", map[string]any{"op": "install", "name": "dlv"}, "no language server"},
		{"dap_adapters", map[string]any{"op": "install", "name": "gopls"}, "no debug adapter"},
		{"extensions", map[string]any{"op": "run"}, "needs id"},
		{"extensions", map[string]any{"op": "log"}, ""},
		{"workspace_folders", map[string]any{"op": "add"}, "needs path"},
		{"workspace_folders", map[string]any{"op": "open", "path": "/tmp"}, "restarts termocode"},
	}
	for _, c := range cases {
		err := aiAdminTool(t, c.tool).Validate(c.args)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s %v: unexpected error %v", c.tool, c.args, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s %v: error %v, want %q", c.tool, c.args, err, c.want)
		}
	}
}

// K8: the palette policy points to the typed tools that replace the
// never commands.
func TestAIPolicyPointsToK8Tools(t *testing.T) {
	for id, tool := range map[string]string{
		"lsp-manage":              "lsp_servers",
		"dap-install":             "dap_adapters",
		"workspace-add-folder":    "workspace_folders",
		"workspace-remove-folder": "workspace_folders",
		"open-workspace":          "workspace_folders",
	} {
		p, ok := aiActionPolicyFor(id)
		if !ok || !strings.Contains(p.Reason, tool) {
			t.Errorf("%s: reason %q does not name %s", id, p.Reason, tool)
		}
	}
}

// K8: the docs catalog is built from the live tool list: every tool is
// there with its arguments and a gate.
func TestAIToolCatalog(t *testing.T) {
	cat := AIToolCatalog()
	byName := map[string]AIToolDoc{}
	for _, d := range cat {
		if d.Gate == "" {
			t.Errorf("%s has no gate", d.Name)
		}
		byName[d.Name] = d
	}
	for _, n := range []string{"read_file", "open_file", "execute_action", "lsp_nav", "lsp_rename", "tasks_run",
		"lsp_servers", "dap_adapters", "extensions", "workspace_folders"} {
		if _, ok := byName[n]; !ok {
			t.Errorf("catalog has no %s", n)
		}
	}
	if g := byName["lsp_servers"].Gate; !strings.Contains(g, "install: confirm (run)") || !strings.Contains(g, "list: auto") {
		t.Errorf("lsp_servers gate %q", g)
	}
	if g := byName["read_file"].Gate; g != "auto" {
		t.Errorf("read_file gate %q", g)
	}
	var op *AIToolArg
	for i, a := range byName["lsp_servers"].Args {
		if a.Name == "op" {
			op = &byName["lsp_servers"].Args[i]
		}
	}
	if op == nil || !op.Required || !reflect.DeepEqual(op.Enum, aiInstallOps) {
		t.Errorf("lsp_servers op arg %+v", op)
	}
	pol := AIActionPolicyCatalog()
	if len(pol) < 100 {
		t.Fatalf("only %d policy rows", len(pol))
	}
	for _, p := range pol {
		if p.Gate == "never" && p.Reason == "" {
			t.Errorf("%s: never without a reason", p.ID)
		}
	}
}
