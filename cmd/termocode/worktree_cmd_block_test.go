package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// CMP-80: `termocode worktree block|unblock <worktree>` records what the
// current worktree waits for; list shows it.
func TestRunWorktreeBlock(t *testing.T) {
	var got []agentctl.Request
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(_ string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) {
		got = append(got, r)
		if r.Op == agentctl.OpWorktreeList {
			return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{{Path: "/w/b", Branch: "feat/b", BlockedBy: []string{"feat/a"}}}}, nil
		}
		return agentctl.Reply{OK: true}, nil
	}
	run := func(args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runWorktreeIn(args, &out, &errw, func(string) string { return "" }, func() (string, error) { return "/w/b", nil })
		return code, out.String(), errw.String()
	}
	if code, _, e := run("block", "feat/a"); code != 0 || e != "" {
		t.Fatalf("block: %d %q", code, e)
	}
	if r := got[0]; r.Op != agentctl.OpWorktreeBlock || r.Text != "feat/a" || r.Dir != "/w/b" {
		t.Errorf("block request %+v", r)
	}
	if code, _, _ := run("unblock", "feat/a"); code != 0 || got[1].Op != agentctl.OpWorktreeUnblock || got[1].Text != "feat/a" {
		t.Errorf("unblock: %d %+v", code, got[1])
	}
	if code, _, _ := run("block"); code != 2 {
		t.Errorf("block without a worktree: %d", code)
	}
	if code, out, _ := run("list"); code != 0 || !strings.Contains(out, "WAITS FOR") || !strings.Contains(out, "feat/a") {
		t.Errorf("list:\n%s", out)
	}
	if code, out, _ := run("--help"); code != 0 || !strings.Contains(out, "termocode worktree block") {
		t.Errorf("help: %s", out)
	}
}
