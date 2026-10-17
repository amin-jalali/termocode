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
