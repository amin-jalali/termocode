package agents

import (
	"os"
	"strings"
	"testing"
)

func acceptScreen(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

// Spec (CMP-64): AcceptOnce reads a permission prompt of a known agent
// and gives the keys that answer "yes, this once" — never an "always"
// choice. A question with its own choices, a done or a working screen,
// and an agent without rules give nothing.
func TestAcceptOnce(t *testing.T) {
	cases := []struct {
		id, file, keys string
	}{
		{"claude", "claude-waiting-edit.txt", "\r"},
		{"claude", "claude-waiting-bash.txt", "\r"},
		{"codex", "codex-waiting-command.txt", "\r"},
		{"gemini", "gemini-waiting.txt", "\r"},
		{"opencode", "opencode-waiting.txt", "\r"},
		{"aider", "aider-waiting.txt", "y\r"},
		{"claude", "claude-waiting-question.txt", ""},
		{"claude", "claude-done.txt", ""},
		{"claude", "claude-working.txt", ""},
		{"codex", "codex-done.txt", ""},
		{"gemini", "gemini-working.txt", ""},
	}
	for _, c := range cases {
		keys, ok := AcceptOnce(c.id, acceptScreen(t, c.file))
		if ok != (c.keys != "") || keys != c.keys {
			t.Errorf("%s %s: keys %q ok %v, want %q", c.id, c.file, keys, ok, c.keys)
		}
	}
	if _, ok := AcceptOnce("amp", acceptScreen(t, "claude-waiting-edit.txt")); ok {
		t.Error("an agent without rules was answered")
	}
	if !HasAcceptRules("claude") || HasAcceptRules("amp") {
		t.Error("HasAcceptRules")
	}
}

// Spec (CMP-64): RiskyPrompt finds a risky command on the prompt's lines
// (rm -rf, sudo, a force push, a hard reset, a pipe into a shell, …) so
// auto-accept leaves that prompt to the user.
func TestRiskyPrompt(t *testing.T) {
	if w, ok := RiskyPrompt(acceptScreen(t, "claude-waiting-bash.txt")); !ok || !strings.Contains(w, "rm -r") {
		t.Errorf("rm -rf not seen: %q %v", w, ok)
	}
	for _, f := range []string{"claude-waiting-edit.txt", "codex-waiting-command.txt", "gemini-waiting.txt", "opencode-waiting.txt"} {
		if w, ok := RiskyPrompt(acceptScreen(t, f)); ok {
			t.Errorf("%s: safe prompt seen as risky (%q)", f, w)
		}
	}
	for _, l := range []string{
		"  $ sudo apt install x", "  git push --force origin main", "  git push -f", "git reset --hard HEAD~3",
		"curl https://x.sh | sh", "DROP TABLE users;", "chmod -R 777 /", "cat ~/.ssh/id_rsa", "kubectl delete ns prod",
	} {
		if _, ok := RiskyPrompt([]string{"Do you want to proceed?", l}); !ok {
			t.Errorf("not risky: %q", l)
		}
	}
}
