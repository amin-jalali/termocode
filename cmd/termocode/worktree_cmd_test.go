package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// WTX-12: `termocode worktree note|status|list|link` send the folder the
// command runs in (and the tab) to termocode's socket; a bad status is
// refused before anything is sent.
func TestRunWorktree(t *testing.T) {
	var got []agentctl.Request
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(_ string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) {
		got = append(got, r)
		if r.Op == agentctl.OpWorktreeList {
			return agentctl.Reply{OK: true, Worktrees: []agentctl.Worktree{{Path: "/src/app.worktrees/x", Branch: "feat/x", Checkpoint: "blocked", Note: "waiting on review", Link: "#412 open"}}}, nil
		}
		return agentctl.Reply{OK: true}, nil
	}
	env := map[string]string{"TERMOCODE_SOCKET": "/s/a.sock", "TERMOCODE_TAB_ID": "t3"}
	run := func(args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runWorktreeIn(args, &out, &errw, func(k string) string { return env[k] }, func() (string, error) { return "/src/app.worktrees/x/web", nil })
		return code, out.String(), errw.String()
	}
	if code, _, e := run("note", "adding", "the", "pin", "row"); code != 0 || e != "" {
		t.Fatalf("note: %d %q", code, e)
	}
	if r := got[0]; r.Op != agentctl.OpWorktreeNote || r.Text != "adding the pin row" || r.Dir != "/src/app.worktrees/x/web" || r.Tab != "t3" {
		t.Errorf("note request %+v", r)
	}
	if code, _, _ := run("status", "blocked"); code != 0 || got[1].Op != agentctl.OpWorktreeStatus || got[1].State != "blocked" {
		t.Errorf("status: %d %+v", code, got[1])
	}
	if code, _, e := run("status", "waiting"); code == 0 || !strings.Contains(e, "working, blocked, done") || len(got) != 2 {
		t.Errorf("bad status sent: %d %q %d", code, e, len(got))
	}
	if code, out, _ := run("list"); code != 0 || !strings.Contains(out, "feat/x") || !strings.Contains(out, "blocked") || !strings.Contains(out, "#412 open") {
		t.Errorf("list: %d\n%s", code, out)
	}
	if code, out, _ := run("list", "--json"); code != 0 || !strings.Contains(out, `"checkpoint":"blocked"`) {
		t.Errorf("list --json: %s", out)
	}
	if code, _, _ := run("link", "412"); code != 0 || got[len(got)-1].Op != agentctl.OpWorktreeLink || got[len(got)-1].Text != "412" {
		t.Errorf("link: %d %+v", code, got[len(got)-1])
	}
	if code, _, _ := run("note"); code != 2 {
		t.Errorf("note without text: %d", code)
	}
	if code, out, _ := run("--help"); code != 0 || !strings.Contains(out, "termocode worktree note") {
		t.Errorf("help: %d %s", code, out)
	}
}
