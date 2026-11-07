package agents

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/lspinstall"
)

// Spec (AGX-3): a preset with a one-click install names an agent-CLI
// tool of the installer registry with the same program; it installs with
// npm or pip into termocode's tools folder only — never curl, a shell
// pipe, sudo or a global install.
func TestInstallToolsAreSafe(t *testing.T) {
	want := map[string]string{
		"claude": "claude-code", "codex": "codex", "gemini": "gemini-cli", "opencode": "opencode",
		"aider": "aider", "amp": "amp", "qwen": "qwen-code", "crush": "crush", "copilot": "copilot-cli",
		"kimi": "kimi-cli", "pi": "pi",
		"auggie": "auggie", "grok": "grok-cli", "cline": "cline", "continue": "continue-cli", "vibe": "mistral-vibe",
	}
	for _, a := range Registry {
		if a.Tool != want[a.ID] {
			t.Errorf("%s: Tool = %q, want %q", a.ID, a.Tool, want[a.ID])
		}
		if a.Tool == "" {
			continue
		}
		tl, ok := lspinstall.Lookup(a.Tool)
		if !ok {
			t.Errorf("%s: no installer tool %q", a.ID, a.Tool)
			continue
		}
		if tl.Category != lspinstall.CategoryAgentCLI || tl.Bin != a.Bin {
			t.Errorf("%s: tool %+v", a.ID, tl)
		}
		if tl.InstallKind != lspinstall.KindNpm && tl.InstallKind != lspinstall.KindPip {
			t.Errorf("%s: install kind %s", a.ID, tl.InstallKind)
		}
		if tl.InstallKind == lspinstall.KindNpm && !contains(a.Packages, tl.Spec) {
			t.Errorf("%s: npm %s is not in Packages %v (npx detection)", a.ID, tl.Spec, a.Packages)
		}
		p, err := lspinstall.BuildPlan(tl, "/tools/"+tl.Name, "linux", "amd64")
		if err != nil {
			t.Fatalf("%s: %v", a.ID, err)
		}
		for _, st := range p.Steps {
			argv := " " + strings.Join(st.Argv, " ") + " "
			for _, bad := range []string{"curl", "wget", "|", " sh ", " bash ", "sudo", " -g ", "--global"} {
				if strings.Contains(argv, bad) {
					t.Errorf("%s: step %q has %q", a.ID, argv, bad)
				}
			}
			if !strings.Contains(argv, "/tools/"+tl.Name) && st.Argv[0] != "npm" {
				t.Errorf("%s: step %q leaves the tools folder", a.ID, argv)
			}
		}
	}
	if n := len(lspinstall.ByCategory(lspinstall.CategoryAgentCLI)); n != len(want) {
		t.Errorf("%d agent CLI tools, want %d", n, len(want))
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
