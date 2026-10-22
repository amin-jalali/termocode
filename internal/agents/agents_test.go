package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (U8 / B4): every sample screen in testdata/<agent>-<state>[-x].txt
// is read as that state. The samples are the bottom of each agent's
// terminal screen: a permission or question prompt is "waiting", a
// spinner with its interrupt hint is "working", the idle input box is
// "done".
func TestDetectSamples(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.txt")
	if len(files) < 15 {
		t.Fatalf("only %d samples", len(files))
	}
	seen := map[string]bool{}
	for _, f := range files {
		parts := strings.Split(strings.TrimSuffix(filepath.Base(f), ".txt"), "-")
		id, want := parts[0], parts[1]
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := ByID(id); !ok {
			t.Errorf("%s: unknown agent %q", f, id)
			continue
		}
		got := Detect(id, strings.Split(string(b), "\n"))
		if got.String() != want {
			t.Errorf("%s: Detect = %s, want %s", f, got, want)
		}
		seen[id+"-"+want] = true
	}
	for _, a := range Registry {
		for _, st := range []string{"waiting", "working", "done"} {
			if !seen[a.ID+"-"+st] {
				t.Errorf("no %s sample for %s", st, a.ID)
			}
		}
	}
}

// Spec: a screen with nothing known (startup noise, a blank screen) is
// Unknown, so the caller keeps the last state instead of flapping.
func TestDetectUnknown(t *testing.T) {
	for _, a := range Registry {
		if s := Detect(a.ID, []string{"", "loading…", ""}); s != Unknown {
			t.Errorf("%s: %s on a blank screen", a.ID, s)
		}
		if s := Detect(a.ID, nil); s != Unknown {
			t.Errorf("%s: %s on no lines", a.ID, s)
		}
	}
	// A plain [y/n] question is "waiting" for every agent.
	for _, a := range Registry {
		if s := Detect(a.ID, []string{"Overwrite go.mod? [y/N] "}); s != Waiting {
			t.Errorf("%s: [y/N] is %s", a.ID, s)
		}
	}
	if s := Detect("nope", []string{"esc to interrupt"}); s != Unknown {
		t.Errorf("unknown agent: %s", s)
	}
}

// Spec: a prompt that scrolled far up (older than the last screen
// lines) does not count.
func TestDetectOnlyTheBottom(t *testing.T) {
	lines := []string{"Do you want to proceed?", "❯ 1. Yes", "  2. No (esc)"}
	for i := 0; i < 40; i++ {
		lines = append(lines, "output line")
	}
	lines = append(lines, "╭────╮", "│ >  │", "╰────╯", "  ? for shortcuts")
	if s := Detect("claude", lines); s != Done {
		t.Errorf("old prompt still counts: %s", s)
	}
}

// Spec: the agent is found from the command line the shell reports
// (OSC 133 C): the first word after variable assignments and a few
// wrappers, by its file name.
func TestFromCommand(t *testing.T) {
	cases := map[string]string{
		"claude":                          "claude",
		"claude --continue":               "claude",
		"/home/u/.local/bin/claude -p hi": "claude",
		"DEBUG=1 FOO=bar codex":           "codex",
		"exec gemini":                     "gemini",
		"command opencode run":            "opencode",
		"npx @anthropic-ai/claude-code":   "claude",
		"npx -y @google/gemini-cli":       "gemini",
		"bunx @openai/codex":              "codex",
		"aider --model sonnet":            "aider",
		"  claude":                        "claude",
		"echo claude":                     "",
		"vim claude.md":                   "",
		"":                                "",
		"claude-ish":                      "",
		"sudo claude":                     "",
		"time claude":                     "claude",
		"nice -n 10 claude":               "",
	}
	for cmd, want := range cases {
		a, ok := FromCommand(cmd)
		if got := a.ID; got != want || ok != (want != "") {
			t.Errorf("FromCommand(%q) = %q, %v; want %q", cmd, got, ok, want)
		}
	}
}

// The registry has the four palette agents plus aider, each with a
// title and a binary.
func TestRegistry(t *testing.T) {
	ids := []string{}
	for _, a := range Registry {
		if a.ID == "" || a.Title == "" || a.Bin == "" {
			t.Errorf("incomplete %+v", a)
		}
		ids = append(ids, a.ID)
	}
	if strings.Join(ids, ",") != "claude,codex,gemini,opencode,aider" {
		t.Errorf("ids %v", ids)
	}
	if a, _ := ByID("claude"); a.Title != "Claude Code" || !a.Palette {
		t.Errorf("claude %+v", a)
	}
	if a, _ := ByID("aider"); a.Palette {
		t.Error("aider is not a palette entry")
	}
}

// Spec (MS-4): the launch line of each agent, golden. The temp file's
// path goes to Claude Code; Codex takes -c overrides; the others have no
// per-launch flag.
func TestMCPLineGolden(t *testing.T) {
	const exe, root, file = "/opt/tc/termocode", "/home/u/proj", "/run/tc/k.mcp-claude.json"
	want := map[string]string{
		"claude": `claude --mcp-config /run/tc/k.mcp-claude.json`,
		"codex":  `codex -c 'mcp_servers.termocode.command="/opt/tc/termocode"' -c 'mcp_servers.termocode.args=["mcp", "--dir", "/home/u/proj"]'`,
	}
	for _, a := range Registry {
		line, ok := a.MCPLine(exe, root, file)
		w, has := want[a.ID]
		if ok != has || line != w {
			t.Errorf("%s: ok=%v line=%q, want %v %q", a.ID, ok, line, has, w)
		}
	}
	if a, _ := ByID("claude"); a.MCP.TempFile == "" {
		t.Error("claude takes a file")
	}
	if a, _ := ByID("opencode"); a.MCP.Kind != MCPProjectFile || a.MCP.ProjectFile != "opencode.json" {
		t.Errorf("opencode: %+v", a.MCP)
	}
	for _, id := range []string{"gemini", "aider"} {
		if a, _ := ByID(id); a.MCP.Kind != MCPNone {
			t.Errorf("%s: %+v", id, a.MCP)
		}
	}
}

// Spec (MS-4): a path with a quote or a control character is refused, not
// guessed at; spaces and TOML specials are quoted.
func TestMCPLineQuoting(t *testing.T) {
	a, _ := ByID("codex")
	if _, ok := a.MCPLine("/x/it's", "/r", ""); ok {
		t.Error("single quote accepted")
	}
	if c, _ := ByID("claude"); true {
		if _, ok := c.MCPLine("/x", "/r", "/f\n"); ok {
			t.Error("newline accepted")
		}
	}
	line, ok := a.MCPLine("/x/a b", `/r"\q`, "")
	if !ok || !strings.Contains(line, `command="/x/a b"'`) || !strings.Contains(line, `"/r\"\\q"]'`) {
		t.Errorf("quoting: %v %s", ok, line)
	}
	if w, _ := ShellWord("/plain/path-1.2"); w != "/plain/path-1.2" {
		t.Errorf("plain word quoted: %s", w)
	}
}
