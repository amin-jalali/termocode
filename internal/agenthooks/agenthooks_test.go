package agenthooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Spec: the claude plan adds one command hook per event, keeps the
// user's keys and hooks, is a no-op once installed, and removes only its
// own hooks; Apply keeps a backup and the file mode.
func TestClaudeHooks(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	write(t, path, `{"model":"x","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`)
	p, err := PlanInstall("claude", home, "/opt/my bin/termocode")
	if err != nil || p.Installed || len(p.Lines) != 4 || !strings.Contains(strings.Join(p.Lines, "\n"), "+ hooks.Notification: '/opt/my bin/termocode' agent report waiting --agent claude") {
		t.Fatalf("plan %+v %v", p, err)
	}
	if err := Apply(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path + ".termocode-backup"); !strings.Contains(string(b), "say done") {
		t.Error("no backup")
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	var doc map[string]any
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &doc) != nil || doc["model"] != "x" || !strings.Contains(string(b), "say done") || !strings.Contains(string(b), "report done --agent claude") {
		t.Fatalf("file %s", b)
	}
	p, _ = PlanInstall("claude", home, "/opt/my bin/termocode")
	if !p.Installed || p.New != nil {
		t.Fatalf("second install %+v", p)
	}
	p, _ = PlanRemove("claude", home)
	if len(p.Lines) != 4 || Apply(p) != nil {
		t.Fatalf("remove %+v", p)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "termocode") || !strings.Contains(string(b), "say done") {
		t.Fatalf("after remove %s", b)
	}
	write(t, path, "{ // comment\n}")
	if _, err := PlanInstall("claude", home, "/opt/my bin/termocode"); err == nil {
		t.Error("a file that is not plain JSON must not be rewritten")
	}
}

// Spec: the codex plan puts one notify line first (before any table),
// refuses to replace the user's own notify, and removes only its line.
func TestCodexHooks(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	p, err := PlanInstall("codex", home, "/usr/bin/termocode")
	if err != nil || len(p.Lines) != 1 {
		t.Fatalf("new file %+v %v", p, err)
	}
	write(t, path, "model = \"o4\"\n[mcp_servers.x]\ncommand = \"y\"\n")
	p, _ = PlanInstall("codex", home, "/usr/bin/termocode")
	if err := Apply(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), `notify = ["/usr/bin/termocode", "agent", "report", "done", "--agent", "codex"]`+"\n") || !strings.Contains(string(b), "[mcp_servers.x]") {
		t.Fatalf("file %s", b)
	}
	if p, _ := PlanInstall("codex", home, "/usr/bin/termocode"); !p.Installed || p.New != nil {
		t.Fatalf("second install %+v", p)
	}
	p, _ = PlanRemove("codex", home)
	Apply(p)
	if b, _ := os.ReadFile(path); string(b) != "model = \"o4\"\n[mcp_servers.x]\ncommand = \"y\"\n" {
		t.Fatalf("after remove %q", b)
	}
	write(t, path, "notify = [\"my-notifier\"]\n")
	if _, err := PlanInstall("codex", home, "/usr/bin/termocode"); err == nil {
		t.Error("the user's own notify was replaced")
	}
	if _, err := PlanInstall("gemini", home, ""); err == nil {
		t.Error("unknown agent")
	}
}
