package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// Spec (CMP-39): `termocode agent start` sends a task-start with the
// task, the flags and the current folder, waits long enough for the
// user's answer and prints the new worktree; Cancel is exit 1.
func TestRunAgentStart(t *testing.T) {
	var got []agentctl.Request
	var waits []time.Duration
	cancel := false
	old, oldWd := agentCall, agentGetwd
	t.Cleanup(func() { agentCall, agentGetwd = old, oldWd })
	agentGetwd = func() (string, error) { return "/src/app", nil }
	agentCall = func(_ string, r agentctl.Request, d time.Duration) (agentctl.Reply, error) {
		got, waits = append(got, r), append(waits, d)
		if cancel {
			return agentctl.Reply{Error: "cancelled: the user made nothing"}, errors.New("cancelled: the user made nothing")
		}
		return agentctl.Reply{OK: true, State: "started", Worktrees: []agentctl.Worktree{{Path: "/src/app.worktrees/task-fix", Branch: "task/fix", Agent: "codex"}}}, nil
	}
	getenv := func(k string) string { return map[string]string{"TERMOCODE_SOCKET": "/s/a.sock"}[k] }
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runAgentIn(args, strings.NewReader(stdin), &out, &errw, getenv)
		return code, out.String(), errw.String()
	}
	code, out, _ := run("", "start", "--agent", "codex", "--name=fix", "--from", "main", "fix", "the", "tests")
	if code != 0 || strings.TrimSpace(out) != "/src/app.worktrees/task-fix" {
		t.Fatalf("start %d %q", code, out)
	}
	r := got[len(got)-1]
	if r.Op != agentctl.OpTaskStart || r.Text != "fix the tests" || r.Agent != "codex" || r.Name != "fix" || r.From != "main" || r.Dir != "/src/app" {
		t.Errorf("request %+v", r)
	}
	if waits[len(waits)-1] < agentctl.TaskStartTimeout {
		t.Errorf("waits only %v for the user", waits[len(waits)-1])
	}
	if code, _, _ := run("from stdin\n", "start", "-"); code != 0 || got[len(got)-1].Text != "from stdin\n" {
		t.Errorf("start - %+v", got[len(got)-1])
	}
	if code, out, _ := run("", "start", "--json", "x"); code != 0 || !strings.Contains(out, `"state":"started"`) || !strings.Contains(out, `"agent":"codex"`) {
		t.Errorf("--json %q", out)
	}
	n := len(got)
	if code, _, _ := run("", "start"); code != 2 || len(got) != n {
		t.Error("no task must be usage")
	}
	if code, _, _ := run("", "start", "--bogus", "x"); code != 2 {
		t.Error("unknown flag must be usage")
	}
	cancel = true
	if code, _, errs := run("", "start", "x"); code != 1 || !strings.Contains(errs, "cancelled") {
		t.Errorf("cancel %d %q", code, errs)
	}
}

// Spec (CMP-39): `termocode agent tasks` lists only the worktrees with an
// agent.
func TestRunAgentTasks(t *testing.T) {
	old, oldWd := agentCall, agentGetwd
	t.Cleanup(func() { agentCall, agentGetwd = old, oldWd })
	agentGetwd = func() (string, error) { return "/src/app", nil }
	var got agentctl.Request
	agentCall = func(_ string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) {
		got = r
		return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{
			{Path: "/src/app", Branch: "main", Kind: "main"},
			{Path: "/src/app.worktrees/task-fix", Branch: "task/fix", Kind: "user", Agent: "codex", Checkpoint: "working", Note: "half way"},
			{Path: "/src/app/.termocode/agents/a1", Branch: "agent/a1", Kind: "agent"},
		}}, nil
	}
	var out, errw bytes.Buffer
	if code := runAgentIn([]string{"tasks"}, strings.NewReader(""), &out, &errw, func(string) string { return "/s/a.sock" }); code != 0 {
		t.Fatalf("tasks %d %s", code, errw.String())
	}
	s := out.String()
	if got.Op != agentctl.OpWorktreeList || got.Dir != "/src/app" {
		t.Errorf("request %+v", got)
	}
	if !strings.Contains(s, "task/fix") || !strings.Contains(s, "codex") || !strings.Contains(s, "half way") || !strings.Contains(s, "agent/a1") || strings.Contains(s, "main ") {
		t.Errorf("tasks:\n%s", s)
	}
	out.Reset()
	if code := runAgentIn([]string{"tasks", "--json"}, strings.NewReader(""), &out, &errw, func(string) string { return "/s/a.sock" }); code != 0 || strings.Count(out.String(), `"path"`) != 2 {
		t.Errorf("--json %q", out.String())
	}
}
