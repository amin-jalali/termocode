package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentroll"
	"github.com/amin-jalali/termocode/internal/agents"
)

// OMX-35: this window writes its agents' states when they change (a
// coding CLI that waits for you), and drops the file when nothing works.
func TestAgentRollupWritesOnChange(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	m.terminalTabs = []terminalTab{{Agent: "claude", AgentState: agents.Waiting}}
	m.agentRollupSync()
	dir, _ := agentroll.Dir()
	b, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(os.Getpid())+".json"))
	if err != nil || !strings.Contains(string(b), `"state":"waiting"`) {
		t.Fatalf("roll-up %s %v", b, err)
	}
	m.terminalTabs[0].AgentState = agents.Done
	m.agentRollupSync()
	if _, err := os.Stat(filepath.Join(dir, strconv.Itoa(os.Getpid())+".json")); !os.IsNotExist(err) {
		t.Fatal("an idle window keeps its roll-up")
	}
}

// OMX-35: the Runs view lists other workspaces whose agents work or
// wait for you.
func TestRunsShowsOtherWorkspaces(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := runsRig(t, "", 120, 40)
	dir, _ := agentroll.Dir()
	if err := agentroll.Write(dir, agentroll.Rollup{Workspace: "/home/u/shop", PID: os.Getppid(),
		Items: []agentroll.Item{{Name: "Claude Code", State: "waiting"}, {Name: "run x", State: "working"}}}); err != nil {
		t.Fatal(err)
	}
	m.runs.others = agentRollupOthers()
	wantAll(t, runsSideText(m), "OTHER WORKSPACES", "shop · 1 waiting for you")
}
