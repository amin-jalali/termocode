package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (AGX-1): ids, programs and npx package names are unique, so a
// command line names one agent only.
func TestRegistryNoDuplicates(t *testing.T) {
	ids, bins, pkgs := map[string]bool{}, map[string]bool{}, map[string]string{}
	for _, a := range Registry {
		if ids[a.ID] {
			t.Errorf("id %s twice", a.ID)
		}
		if bins[a.Bin] {
			t.Errorf("bin %s twice", a.Bin)
		}
		ids[a.ID], bins[a.Bin] = true, true
		for _, p := range a.Packages {
			if other, dup := pkgs[p]; dup {
				t.Errorf("package %s in %s and %s", p, other, a.ID)
			}
			pkgs[p] = a.ID
		}
	}
	if len(Registry) < 14 {
		t.Errorf("only %d presets", len(Registry))
	}
}

// Spec (AGX-1): every preset is found from its own command line, from
// npx with its package, and with arguments.
func TestFromCommandEveryPreset(t *testing.T) {
	for _, a := range Registry {
		for _, cmd := range []string{a.Bin, a.Bin + " --help", "/usr/local/bin/" + a.Bin, "FOO=1 " + a.Bin} {
			if got, ok := FromCommand(cmd); !ok || got.ID != a.ID {
				t.Errorf("FromCommand(%q) = %q, %v; want %s", cmd, got.ID, ok, a.ID)
			}
		}
		for _, p := range a.Packages {
			if got, ok := FromCommand("npx -y " + p + "@latest"); !ok || got.ID != a.ID {
				t.Errorf("npx %s = %q, %v; want %s", p, got.ID, ok, a.ID)
			}
		}
	}
}

// Spec (AGX-1): a preset without sample screens has no screen rules and
// its state is Unknown for every screen — the samples of the other
// agents, a [y/N] question, an interrupt hint — never a wrong "done" or
// "waiting".
func TestPresetsWithoutRulesStayUnknown(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.txt")
	var screens [][]string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		screens = append(screens, strings.Split(string(b), "\n"))
	}
	screens = append(screens, []string{"Overwrite go.mod? [y/N] "}, []string{"esc to interrupt"}, []string{"> "})
	without := 0
	for _, a := range Registry {
		if HasScreenRules(a.ID) {
			continue
		}
		without++
		for i, sc := range screens {
			if s := Detect(a.ID, sc); s != Unknown {
				t.Errorf("%s: screen %d is %s, want unknown", a.ID, i, s)
			}
		}
	}
	if without == 0 {
		t.Error("every preset has rules; the test checks nothing")
	}
	for _, id := range []string{"claude", "codex", "gemini", "opencode", "aider"} {
		if !HasScreenRules(id) {
			t.Errorf("%s lost its screen rules", id)
		}
	}
	if HasScreenRules("nope") {
		t.Error("an unknown id has rules")
	}
}

// Spec (AGX-1): the first state of a new agent tab is Working for a
// preset with screen rules (the screen corrects it) and Unknown for one
// without (no mark that could be wrong).
func TestStartState(t *testing.T) {
	for _, a := range Registry {
		want := Unknown
		if HasScreenRules(a.ID) {
			want = Working
		}
		if got := StartState(a.ID); got != want {
			t.Errorf("%s: StartState = %s, want %s", a.ID, got, want)
		}
	}
}

// Spec (AGX-3): the sign-in line is the CLI's own documented command,
// a plain program and words (no shell pipe, no redirect, no sudo), and
// starts with the agent's program.
func TestLoginLines(t *testing.T) {
	want := map[string]string{
		"claude": "claude auth login", "codex": "codex login", "opencode": "opencode auth login",
		"cursor": "cursor-agent login", "amp": "amp login", "goose": "goose configure",
		"kiro": "kiro-cli login", "auggie": "auggie login",
	}
	for _, a := range Registry {
		if a.Login != want[a.ID] {
			t.Errorf("%s: Login = %q, want %q", a.ID, a.Login, want[a.ID])
		}
		if a.Login == "" {
			continue
		}
		if !strings.HasPrefix(a.Login, a.Bin+" ") || strings.ContainsAny(a.Login, "|;&<>$`\\'\"") || strings.Contains(a.Login, "sudo") {
			t.Errorf("%s: unsafe login line %q", a.ID, a.Login)
		}
	}
}

// Spec (CMP-3): more agent CLIs are presets, each with its own program,
// and the ones a package manager installs have a one-click install tool.
func TestMorePresets(t *testing.T) {
	for _, c := range []struct{ id, bin, pkg, tool string }{
		{"kiro", "kiro-cli", "", ""},
		{"auggie", "auggie", "@augmentcode/auggie", "auggie"},
		{"grok", "grok", "@vibe-kit/grok-cli", "grok-cli"},
		{"cline", "cline", "cline", "cline"},
		{"continue", "cn", "@continuedev/cli", "continue-cli"},
		{"vibe", "vibe", "", "mistral-vibe"},
	} {
		a, ok := ByID(c.id)
		if !ok {
			t.Errorf("%s: no preset", c.id)
			continue
		}
		if a.Bin != c.bin || a.Tool != c.tool || a.Palette {
			t.Errorf("%s = %+v", c.id, a)
		}
		if c.pkg != "" && (len(a.Packages) != 1 || a.Packages[0] != c.pkg) {
			t.Errorf("%s packages %v, want %s", c.id, a.Packages, c.pkg)
		}
	}
	if len(Registry) < 20 {
		t.Errorf("only %d presets", len(Registry))
	}
}

// Spec (CMP-3): a preset with no package-manager install names its own
// install page (https), so Enter on it can open the page instead of a
// dead end; a preset with a one-click install needs none.
func TestInstallPages(t *testing.T) {
	for _, a := range Registry {
		if a.Tool != "" {
			continue
		}
		if !strings.HasPrefix(a.InstallURL, "https://") || strings.ContainsAny(a.InstallURL, " \t\n'\"") {
			t.Errorf("%s: install page %q", a.ID, a.InstallURL)
		}
	}
}

// Spec (CMP-3): Kimi CLI takes termocode's MCP server for one launch
// from a temp file named by --mcp-config-file (nothing on disk of the
// user's changes).
func TestKimiMCPFlag(t *testing.T) {
	a, _ := ByID("kimi")
	if a.MCP.Kind != MCPFlag || a.MCP.TempFile == "" {
		t.Fatalf("kimi recipe %+v", a.MCP)
	}
	line, ok := a.MCPLine("/bin/termocode", "/w", "/run/kimi.json")
	if !ok || line != "kimi --mcp-config-file /run/kimi.json" {
		t.Errorf("line %q, %v", line, ok)
	}
}
