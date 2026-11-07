package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/webproto"
)

type webLaneStarted struct{ agent, dir, task string }

// webLaneRig is a git repository (the workspace) on a hub with one
// controller; every agent's program is "installed" and starting one is
// recorded instead of opening a terminal tab.
func webLaneRig(t *testing.T) (*webFilesRig, string, *[]webLaneStarted) {
	t.Helper()
	m, dir := aiIDETestModel(t, fakeAIProvider{reply: "x"})
	gitInit(t, dir)
	oldLook, oldStart := agentLookPath, webLaneStartAgent
	t.Cleanup(func() { agentLookPath, webLaneStartAgent = oldLook, oldStart })
	agentLookPath = func(bin string) (string, error) { return "/fake/" + bin, nil }
	var started []webLaneStarted
	webLaneStartAgent = func(_ *Model, a agents.Agent, dir, task string) tea.Cmd {
		started = append(started, webLaneStarted{a.ID, dir, task})
		return nil
	}
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, root: dir, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return r, dir, &started
}

// Spec (MOB-65): ai.lane_agents lists only the installed agents whose
// screen termocode can read (the task is typed for them), with names and
// a sign-in word, never an account.
func TestWebAILaneAgents(t *testing.T) {
	r, _, _ := webLaneRig(t)
	var list webproto.AILaneAgents
	webResData(t, r.call(webproto.OpAILaneAgents, &webproto.EmptyArgs{}), &list)
	if list.NoRepo || len(list.Items) == 0 {
		t.Fatalf("list: %+v", list)
	}
	ids := map[string]bool{}
	for _, it := range list.Items {
		ids[it.ID] = true
		if !agents.HasScreenRules(it.ID) {
			t.Errorf("%s is listed but its screen cannot be read", it.ID)
		}
		if it.State != "ready" && it.State != "signin" && it.State != "unknown" {
			t.Errorf("%s state %q", it.ID, it.State)
		}
	}
	if !ids["claude"] || ids["cursor"] {
		t.Errorf("ids %v", ids)
	}
}

// Spec (MOB-65, mobile plan 3.9): ai.lane_start waits for a laptop card
// (Cancel first) that names the agent and the new folder; nothing is
// made before the laptop says yes. Yes makes a new worktree on a lane
// branch from the current branch and starts the agent in it with the
// task; a second lane for the same task gets its own branch.
func TestWebAILaneStartThroughTheLaptop(t *testing.T) {
	r, dir, started := webLaneRig(t)
	m := r.m
	f := r.call(webproto.OpAILaneStart, &webproto.AILaneStartArgs{Agent: "claude", Task: "fix the\nlogin bug"})
	if f["t"] != "res" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.Buttons()[0].ID != webproto.ChoiceCancel {
		t.Fatalf("lane asked: %v open %v", f, m.confirmOpen)
	}
	folder := dir + ".worktrees/lane-claude-fix-the-login-bug"
	a := m.web.asks.shown
	if a == nil || !strings.Contains(a.body, "Claude Code") || !strings.Contains(a.body, "lane-claude-fix-the-login-bug") || !strings.Contains(a.body, "fix the login bug") {
		t.Fatalf("card: %+v", a)
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("the worktree was made before the laptop said yes")
	}

	m.confirmOpen = false
	if cmd := m.answerWebAsk(webproto.ChoiceCancel); cmd != nil {
		t.Fatal("cancel ran something")
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) || len(*started) != 0 {
		t.Fatal("cancel made a lane")
	}

	r.call(webproto.OpAILaneStart, &webproto.AILaneStartArgs{Agent: "claude", Task: "fix the login bug"})
	m.confirmOpen = false
	webDrain(t, m, m.answerWebAsk(webproto.ChoiceConfirm))
	if _, err := os.Stat(filepath.Join(folder, "a.txt")); err != nil {
		t.Fatalf("no worktree: %v", err)
	}
	if !gitBranchExists(t, dir, "lane/claude-fix-the-login-bug") {
		t.Error("no lane branch")
	}
	if len(*started) != 1 || (*started)[0] != (webLaneStarted{"claude", folder, "fix the login bug"}) {
		t.Fatalf("started %+v", *started)
	}

	r.call(webproto.OpAILaneStart, &webproto.AILaneStartArgs{Agent: "claude", Task: "fix the login bug", From: "main"})
	m.confirmOpen = false
	webDrain(t, m, m.answerWebAsk(webproto.ChoiceConfirm))
	if !gitBranchExists(t, dir, "lane/claude-fix-the-login-bug-2") || len(*started) != 2 {
		t.Errorf("second lane: %+v", *started)
	}
}

// Spec (MOB-65): the laptop refuses, before any card, an agent it does
// not know or whose screen it cannot read, a task that looks like it
// holds a secret, and a start point that is not a branch or tag here.
func TestWebAILaneStartRefusals(t *testing.T) {
	r, _, started := webLaneRig(t)
	for _, c := range []struct {
		args webproto.AILaneStartArgs
		code string
	}{
		{webproto.AILaneStartArgs{Agent: "nope", Task: "x"}, "not_found"},
		{webproto.AILaneStartArgs{Agent: "cursor", Task: "x"}, "bad_request"},
		{webproto.AILaneStartArgs{Agent: "claude", Task: "use key sk-" + "ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}, "bad_request"},
		{webproto.AILaneStartArgs{Agent: "claude", Task: "x", From: "no-such-branch"}, "bad_request"},
	} {
		if got := webErrCode(r.call(webproto.OpAILaneStart, &c.args)); got != c.code {
			t.Errorf("%+v: %q, want %q", c.args, got, c.code)
		}
	}
	if r.m.confirmOpen || len(*started) != 0 {
		t.Error("a refused lane asked or started")
	}
}
