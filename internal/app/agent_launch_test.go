//go:build unix

package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
)

// fakeAgentBin puts a fake program called name first on PATH. It writes
// its argv (one word per line) to the returned file.
func fakeAgentBin(t *testing.T, name string) (rec string) {
	t.Helper()
	bin := t.TempDir()
	rec = filepath.Join(bin, "argv.txt")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + rec + "'\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	return rec
}

// runLaunchLine runs the typed line in a shell, as the agent tab does,
// and returns the fake agent's argv.
func runLaunchLine(t *testing.T, line, rec string) []string {
	t.Helper()
	_ = os.Remove(rec)
	if out, err := exec.Command("sh", "-c", line).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", line, err, out)
	}
	b, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

func agentByID(t *testing.T, id string) agents.Agent {
	t.Helper()
	a, ok := agents.ByID(id)
	if !ok {
		t.Fatal("no agent " + id)
	}
	return a
}

// hashTree lists every file under dir with its hash and mode.
func hashTree(t *testing.T, dir string) string {
	t.Helper()
	var rows []string
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		rows = append(rows, p+" "+fi.Mode().String()+" "+fmt.Sprintf("%x", sha256.Sum256(b)))
		return nil
	})
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

// Spec (MS-4): the line typed for Claude Code and Codex carries the
// per-launch MCP flags; a fake program on PATH sees exactly these words
// (golden). Claude's file is the same server entry as Copy config. With
// mcp_server off the program starts plain. The user's own config files
// (a fake ~/.claude.json, ~/.codex, ~/.config/opencode, ~/.gemini) and
// the workspace stay byte for byte as they were.
func TestAgentLaunchLineArgv(t *testing.T) {
	m := agentMCPTestModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for f, body := range map[string]string{
		".claude.json":                   `{"mcpServers":{"mine":{"command":"x"}}}`,
		".claude/settings.json":          `{}`,
		".codex/config.toml":             "model = \"o\"\n",
		".config/opencode/opencode.json": `{}`,
		".gemini/settings.json":          `{}`,
		"proj/.mcp.json":                 `{}`,
		"proj/.codex/config.toml":        ``,
		"proj/opencode.json":             `{"theme":"x"}`,
	} {
		p := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := hashTree(t, home)
	mcpServeExecutable = func() (string, error) { return "/opt/tc/termocode", nil }
	t.Cleanup(func() { mcpServeExecutable = os.Executable })
	root := aiWorkspaceRoot()

	// claude
	rec := fakeAgentBin(t, "claude")
	line, _ := m.agentLaunchLine(agentByID(t, "claude"))
	argv := runLaunchLine(t, line, rec)
	if len(argv) != 2 || argv[0] != "--mcp-config" {
		t.Fatalf("claude argv %q", argv)
	}
	got, err := os.ReadFile(argv[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mcpServeConfigJSON("/opt/tc/termocode", root) {
		t.Errorf("claude file:\n%s", got)
	}

	// codex
	rec = fakeAgentBin(t, "codex")
	line, _ = m.agentLaunchLine(agentByID(t, "codex"))
	argv = runLaunchLine(t, line, rec)
	want := []string{
		"-c", `mcp_servers.termocode.command="/opt/tc/termocode"`,
		"-c", `mcp_servers.termocode.args=["mcp", "--dir", "` + root + `"]`,
	}
	if strings.Join(argv, "\n") != strings.Join(want, "\n") {
		t.Errorf("codex argv\n%q\nwant\n%q", argv, want)
	}

	// off: plain program, no file
	mcpServeSetMode(t, "off")
	for _, id := range []string{"claude", "codex", "gemini", "opencode", "aider"} {
		a := agentByID(t, id)
		if line, c := m.agentLaunchLine(a); line != a.Bin || c != nil {
			t.Errorf("off: %s → %q", id, line)
		}
	}
	rec = fakeAgentBin(t, "claude")
	if argv := runLaunchLine(t, "claude", rec); len(argv) != 1 || argv[0] != "" {
		t.Errorf("off: claude argv %q", argv)
	}

	if after := hashTree(t, home); after != before {
		t.Errorf("the user's files changed:\n%s\n--\n%s", before, after)
	}
}

// Spec (MS-4): Gemini CLI, OpenCode (before its card) and Aider start
// plain from agentLaunchLine; nothing is written.
func TestAgentLaunchLinePlainAgents(t *testing.T) {
	m := agentMCPTestModel(t)
	for _, id := range []string{"gemini", "aider", "opencode"} {
		a := agentByID(t, id)
		if line, _ := m.agentLaunchLine(a); line != a.Bin {
			t.Errorf("%s: %q", id, line)
		}
	}
	if len(m.agentMCPFiles) != 0 {
		t.Errorf("files %v", m.agentMCPFiles)
	}
}
