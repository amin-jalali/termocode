package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentjail"
	"github.com/amin-jalali/termocode/internal/agents"
)

func fakeJailHost(have ...string) func() agentjail.Host {
	return func() agentjail.Host {
		return agentjail.Host{GOOS: "linux", Home: "/home/u", Tmp: "/tmp", Look: func(n string) (string, error) {
			for _, h := range have {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}}
	}
}

// CMP-60: agent_os_sandbox is a Settings row (off by default) that the
// AI can never change: it is the wall around the outside agents.
func TestAgentOSSandboxSettingRow(t *testing.T) {
	withConfigDir(t)
	cats := settingsCategories()
	for _, e := range SettingsCatalog() {
		if e.Key != "agent_os_sandbox" {
			continue
		}
		if e.Default != "false" || e.ID != "setting-agent-os-sandbox" {
			t.Fatalf("row %+v", e)
		}
		if cats[e.ID] != "Workflows" {
			t.Errorf("category %q", cats[e.ID])
		}
		if g := aiSettingPolicies[e.Key].Gate; g != aiGateNever {
			t.Errorf("AI gate %q, want never", g)
		}
		if agentOSSandboxOn() {
			t.Error("on with no config")
		}
		if err := saveHTTPSetting("agent_os_sandbox", true); err != nil {
			t.Fatal(err)
		}
		if !agentOSSandboxOn() {
			t.Error("still off after saving true")
		}
		return
	}
	t.Fatal("no agent_os_sandbox row")
}

// A race lane types the agent's program, or with the setting on the
// sandbox line around it (interactive: no new session). No sandbox
// tool: the lane does not start.
func TestRaceLaneLineSandbox(t *testing.T) {
	withConfigDir(t)
	old := raceJailHost
	t.Cleanup(func() { raceJailHost = old })
	raceJailHost = fakeJailHost("bwrap")
	a, _ := agents.ByID("claude")
	dir := t.TempDir()

	line, note, err := raceAgentLine(a, dir)
	if err != nil || line != "claude" || note != "" {
		t.Fatalf("off: line=%q note=%q err=%v", line, note, err)
	}
	if err := saveHTTPSetting("agent_os_sandbox", true); err != nil {
		t.Fatal(err)
	}
	line, note, err = raceAgentLine(a, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "'/usr/bin/bwrap' ") || !strings.HasSuffix(line, "'--' 'claude'") || strings.Contains(line, "--new-session") {
		t.Fatalf("on: %s", line)
	}
	if !strings.Contains(line, "'--bind' '"+dir+"' '"+dir+"'") {
		t.Fatalf("the lane's worktree is not writable: %s", line)
	}
	if !strings.Contains(note, "bwrap") {
		t.Fatalf("note %q", note)
	}
	raceJailHost = fakeJailHost()
	if _, _, err := raceAgentLine(a, dir); !errors.Is(err, agentjail.ErrNoTool) {
		t.Fatalf("missing tool: %v", err)
	}
	_ = os.Remove(dir)
}
