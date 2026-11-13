package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

func withAgentCall(t *testing.T, f func(agentctl.Request) (agentctl.Reply, error)) {
	t.Helper()
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(_ string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) { return f(r) }
}

func wtRun(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := runWorktreeIn(args, &out, &errw, func(string) string { return "" }, func() (string, error) { return "/w", nil })
	return code, out.String(), errw.String()
}

func TestWorktreeCommandErrors(t *testing.T) {
	withAgentCall(t, func(r agentctl.Request) (agentctl.Reply, error) {
		switch r.Text {
		case "boom":
			return agentctl.Reply{}, errors.New("not running")
		case "refused":
			return agentctl.Reply{OK: false, Error: "no such worktree"}, nil
		}
		if r.Op == agentctl.OpWorktreeList {
			return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{
				{Path: "/a", Branch: "x", Here: true, Gone: true, Kind: "agent", BlockedBy: []string{"y"}},
				{Path: "/b", BlockedBy: []string{"z"}, Waits: true},
			}}, nil
		}
		return agentctl.Reply{OK: true}, nil
	})
	for _, a := range [][]string{nil, {"status"}, {"status", "a", "b"}, {"link"}, {"block"}, {"unblock", "a", "b"}, {"nonsense"}} {
		if code, _, _ := wtRun(a...); code != 2 {
			t.Errorf("%v: %d", a, code)
		}
	}
	if code, _, errw := wtRun("note", "boom"); code != 1 || !strings.Contains(errw, "not running") {
		t.Errorf("failed call: %d %q", code, errw)
	}
	if code, _, errw := wtRun("block", "refused"); code != 1 || !strings.Contains(errw, "no such worktree") {
		t.Errorf("refused: %d %q", code, errw)
	}
	if code, _, _ := wtRun("unblock", "ok"); code != 0 {
		t.Errorf("unblock: %d", code)
	}
	if code, out, _ := wtRun("list"); code != 0 || !strings.Contains(out, "x *") || !strings.Contains(out, "(gone)") || !strings.Contains(out, "y (done)") {
		t.Errorf("list: %d\n%s", code, out)
	}
	var out, errw bytes.Buffer
	if code := runWorktreeIn([]string{"list"}, &out, &errw, func(string) string { return "" }, func() (string, error) { return "", errors.New("gone") }); code != 1 {
		t.Errorf("no cwd: %d", code)
	}
}

func agentStart(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	fail := func(f string, a ...any) int { errw.WriteString("fail: " + strings.TrimSpace(f) + "\n"); return 1 }
	usage := func() int { errw.WriteString("usage\n"); return 2 }
	code := runAgentStart(args, strings.NewReader(stdin), &out, &errw, fail, usage, "/s")
	return code, out.String(), errw.String()
}

func TestAgentStartAndTasks(t *testing.T) {
	oldWd := agentGetwd
	agentGetwd = func() (string, error) { return "/w", nil }
	t.Cleanup(func() { agentGetwd = oldWd })
	state := "started"
	var worktrees []agentctl.Worktree
	var callErr error
	withAgentCall(t, func(r agentctl.Request) (agentctl.Reply, error) {
		if callErr != nil {
			return agentctl.Reply{}, callErr
		}
		if r.Op == agentctl.OpWorktreeList {
			return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{
				{Path: "/a", Branch: "b", Agent: "claude", Gone: true}, {Path: "/c", Kind: "worktree"},
			}}, nil
		}
		return agentctl.Reply{OK: true, State: state, Worktrees: worktrees}, nil
	})
	worktrees = []agentctl.Worktree{{Path: "/w/new"}}
	if code, out, _ := agentStart(t, "", "--agent=claude", "--name", "n", "--from=main", "--", "do", "--it"); code != 0 || !strings.Contains(out, "/w/new") {
		t.Errorf("start: %d %q", code, out)
	}
	state = "made"
	if code, _, errw := agentStart(t, "", "task"); code != 0 || !strings.Contains(errw, "no agent was started") {
		t.Errorf("not started: %d %q", code, errw)
	}
	if code, out, _ := agentStart(t, "from stdin", "--json", "-"); code != 0 || !strings.Contains(out, `"worktrees"`) {
		t.Errorf("json: %d %q", code, out)
	}
	worktrees = nil
	if code, _, errw := agentStart(t, "", "task"); code != 1 || !strings.Contains(errw, "made no worktree") {
		t.Errorf("no worktree: %d %q", code, errw)
	}
	if code, _, _ := agentStart(t, "", "--bogus"); code != 2 {
		t.Errorf("bad flag: %d", code)
	}
	if code, _, _ := agentStart(t, "  "); code != 2 {
		t.Errorf("no text: %d", code)
	}
	if code, _, errw := agentStart(t, "", "-"); code != 2 {
		t.Errorf("empty stdin: %d %q", code, errw)
	}
	callErr = errors.New("down")
	if code, _, errw := agentStart(t, "", "task"); code != 1 || !strings.Contains(errw, "fail:") {
		t.Errorf("call error: %d %q", code, errw)
	}
	callErr = nil
	var out, errw bytes.Buffer
	fail := func(f string, a ...any) int { return 1 }
	usage := func() int { return 2 }
	if runAgentTasks([]string{"--bogus"}, &out, fail, usage, "/s") != 2 {
		t.Error("tasks flag")
	}
	if runAgentTasks(nil, &out, fail, usage, "/s") != 0 || !strings.Contains(out.String(), "claude") || !strings.Contains(out.String(), "(gone)") {
		t.Errorf("tasks table: %q", out.String())
	}
	out.Reset()
	if runAgentTasks([]string{"--json"}, &out, fail, usage, "/s") != 0 || !strings.Contains(out.String(), "claude") {
		t.Errorf("tasks json: %q", out.String())
	}
	callErr = errors.New("down")
	if runAgentTasks(nil, &out, fail, usage, "/s") != 1 {
		t.Error("tasks call error")
	}
	_ = errw
}

func TestServiceCommandArgs(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runServiceIn([]string{"status", "--dir", t.TempDir()}, strings.NewReader(""), &out, &errw, t.TempDir(), t.TempDir(), "/bin/termocode"); code != 0 && code != 1 {
		t.Errorf("status --dir: %d %s", code, errw.String())
	}
	if code := runServiceIn([]string{"status", "--dir=" + t.TempDir()}, strings.NewReader(""), &out, &errw, t.TempDir(), t.TempDir(), "/bin/termocode"); code != 0 && code != 1 {
		t.Errorf("status --dir=: %d", code)
	}
	if code := runServiceIn(nil, nil, &out, &errw, "", "", ""); code != 2 {
		t.Errorf("no command: %d", code)
	}
	if code := runServiceIn([]string{"--bogus"}, nil, &out, &errw, "", "", ""); code != 2 {
		t.Errorf("bad arg: %d", code)
	}
}

func TestLSPProxyArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--map"}, {"--", "srv"}, {"--map", "/a=/b", "--"}, {"--map", "badmap", "--", "srv"}, {"--bogus"}, {"--map", "/a=/b"},
	} {
		var out, errw bytes.Buffer
		if code := lspProxyMain(args, strings.NewReader(""), &out, &errw); code != 2 {
			t.Errorf("%v: %d %s", args, code, errw.String())
		}
	}
	var out, errw bytes.Buffer
	if code := lspProxyMain([]string{"--help"}, nil, &out, &errw); code != 0 {
		t.Errorf("help: %d", code)
	}
	if code := lspProxyMain([]string{"--map", "/a=/b", "--", "/no/such/server"}, strings.NewReader(""), &out, &errw); code != 1 {
		t.Errorf("missing server: %d %s", code, errw.String())
	}
	if runLSPProxy([]string{"--help"}) != 0 {
		t.Error("runLSPProxy")
	}
}

func TestACPRunsWithAFakeStream(t *testing.T) {
	var out, errw bytes.Buffer
	if code := runACPIn([]string{"bogus"}, strings.NewReader(""), &out, &errw, nil); code != 2 {
		t.Errorf("bad arg: %d", code)
	}
	// With no provider set up the server still starts and ends on EOF.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	errw.Reset()
	if code := runACPIn(nil, strings.NewReader(""), &out, &errw, nil); code != 0 && code != 1 {
		t.Errorf("no provider: %d %s", code, errw.String())
	}
}
