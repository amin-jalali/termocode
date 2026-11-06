package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (AGX-11): the usage-limit message of Claude Code and Codex on the
// bottom of the screen is found (testdata/limit/<agent>-<name>.txt; a
// "none" sample has no limit): LimitLine returns that one line, which
// holds the reset words. A model answer that only talks about limits, an
// agent without limit rules and an empty screen give nothing.
func TestLimitLineSamples(t *testing.T) {
	files, _ := filepath.Glob("testdata/limit/*.txt")
	if len(files) < 4 {
		t.Fatalf("only %d limit samples", len(files))
	}
	for _, f := range files {
		parts := strings.SplitN(strings.TrimSuffix(filepath.Base(f), ".txt"), "-", 2)
		id, name := parts[0], parts[1]
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		line, ok := LimitLine(id, strings.Split(string(b), "\n"))
		if strings.HasPrefix(name, "none") {
			if ok {
				t.Errorf("%s: limit %q on a screen without one", f, line)
			}
			continue
		}
		if !ok || !strings.Contains(strings.ToLower(line), "limit") || !strings.Contains(string(b), line) {
			t.Errorf("%s: LimitLine = %q, %v", f, line, ok)
		}
		if !strings.Contains(line, "reset") && !strings.Contains(line, "try again") {
			t.Errorf("%s: the line %q has no reset words", f, line)
		}
	}
	for _, a := range Registry {
		if _, ok := LimitLine(a.ID, nil); ok {
			t.Errorf("%s: limit on no lines", a.ID)
		}
	}
	claude, _ := os.ReadFile("testdata/limit/claude-usage.txt")
	if _, ok := LimitLine("gemini", strings.Split(string(claude), "\n")); ok {
		t.Error("gemini has no limit rules but matched")
	}
	if !HasLimitRules("claude") || !HasLimitRules("codex") || HasLimitRules("aider") {
		t.Error("HasLimitRules")
	}
}

// Spec (AGX-11): only the bottom of the screen counts: a limit message
// that scrolled up above 20 lines of later output is over.
func TestLimitLineOnlyBottom(t *testing.T) {
	lines := []string{"  ⎿  Claude usage limit reached. Your limit will reset at 2pm."}
	for i := 0; i < 25; i++ {
		lines = append(lines, "⏺ more output")
	}
	if _, ok := LimitLine("claude", lines); ok {
		t.Error("an old limit far above still counts")
	}
}

// Spec (AGX-11): IsPrompt says the screen asks the user (a permission
// prompt or a y/n question): auto-continue never types into it.
func TestIsPrompt(t *testing.T) {
	for _, f := range []string{"testdata/claude-waiting-bash.txt", "testdata/codex-waiting-command.txt"} {
		b, _ := os.ReadFile(f)
		if !IsPrompt(strings.Split(strings.TrimSuffix(f, ".txt")[len("testdata/"):], "-")[0], strings.Split(string(b), "\n")) {
			t.Errorf("%s: not a prompt", f)
		}
	}
	if !IsPrompt("gemini", []string{"Overwrite the file? [y/N]"}) {
		t.Error("a y/n question is a prompt for every agent")
	}
	b, _ := os.ReadFile("testdata/limit/claude-usage.txt")
	if IsPrompt("claude", strings.Split(string(b), "\n")) {
		t.Error("the limit screen is not a prompt")
	}
}
