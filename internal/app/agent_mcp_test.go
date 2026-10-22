//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agents"
)

// agentMCPTestModel is a model with a short private session dir.
func agentMCPTestModel(t *testing.T) *Model {
	t.Helper()
	m := aiTestModel(t)
	mcpServeShortRuntime(t)
	t.Cleanup(removeAgentMCPLive)
	return &m
}

func claudeAgent(t *testing.T) agents.Agent {
	t.Helper()
	a, ok := agents.ByID("claude")
	if !ok {
		t.Fatal("no claude")
	}
	return a
}

// Spec (MS-4): the temp config is the same server entry as Copy config,
// mode 0600 in the 0700 session dir, rewritten at each launch (also over
// a planted link), and holds no secret.
func TestAgentMCPFileWrite(t *testing.T) {
	m := agentMCPTestModel(t)
	root := t.TempDir()
	path, err := m.writeAgentMCPFile(claudeAgent(t), "/opt/tc/termocode", root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := mcpServeConfigJSON("/opt/tc/termocode", root); string(got) != want {
		t.Errorf("contents:\n%s\nwant:\n%s", got, want)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %o", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Dir(path)); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %o", fi.Mode().Perm())
	}
	if !strings.HasSuffix(path, ".mcp-claude.json") {
		t.Errorf("name %s", path)
	}

	// A link planted at the name is replaced, not written through.
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.writeAgentMCPFile(claudeAgent(t), "/opt/tc/termocode", root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("wrote through a link: %q", b)
	}
	if fi, _ := os.Lstat(path); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("still a link")
	}
}

// Spec (MS-4): the file goes when the agent's tab closes (and not
// while another tab of that agent is hidden), and on quit.
func TestAgentMCPFileRemoval(t *testing.T) {
	m := agentMCPTestModel(t)
	root := aiWorkspaceRoot()
	path, err := m.writeAgentMCPFile(claudeAgent(t), "/x/termocode", root)
	if err != nil {
		t.Fatal(err)
	}
	exists := func() bool { _, err := os.Lstat(path); return err == nil }
	m.terminalTabs = []terminalTab{{BufID: 1}, {BufID: 2, Agent: "claude"}, {BufID: 3}}

	m.closeTerminalTab(0)
	if !exists() {
		t.Fatal("removed while the tab is open")
	}
	m.closeTerminalTab(0) // the agent's tab
	if exists() {
		t.Fatal("not removed after the tab closed")
	}

	// Hidden tabs still count.
	path, _ = m.writeAgentMCPFile(claudeAgent(t), "/x/termocode", root)
	m.terminalTabs, m.hiddenTerminalTabs = nil, []terminalTab{{BufID: 2, Agent: "claude"}}
	m.pruneAgentMCPFiles()
	if !exists() {
		t.Fatal("removed while the tab is hidden")
	}
	m.hiddenTerminalTabs = nil
	m.pruneAgentMCPFiles()
	if exists() {
		t.Fatal("hidden tab gone, file stays")
	}

	// Quit, both ways.
	path, _ = m.writeAgentMCPFile(claudeAgent(t), "/x/termocode", root)
	m.terminalTabs = []terminalTab{{BufID: 2, Agent: "claude"}}
	m.quitNow()
	if exists() {
		t.Fatal("quit left the file")
	}
	path, _ = m.writeAgentMCPFile(claudeAgent(t), "/x/termocode", root)
	StopMCPServer()
	if exists() {
		t.Fatal("StopMCPServer left the file")
	}
}
