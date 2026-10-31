package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// Spec (OMX-33/34): the agent CLI sends the right request to the socket
// of TERMOCODE_SOCKET and prints the answer; report takes the tab from
// TERMOCODE_TAB_ID and never fails the hook that runs it.
func TestRunAgent(t *testing.T) {
	var got []agentctl.Request
	var paths []string
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(path string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) {
		got, paths = append(got, r), append(paths, path)
		switch r.Op {
		case agentctl.OpList:
			return agentctl.Reply{OK: true, Tabs: []agentctl.Tab{{ID: "t1", Name: "zsh", Agent: "claude", State: "waiting", Active: true}}}, nil
		case agentctl.OpWait:
			return agentctl.Reply{OK: true, State: r.Until}, nil
		case agentctl.OpReport:
			return agentctl.Reply{}, errors.New("termocode is not running here")
		}
		return agentctl.Reply{OK: true}, nil
	}
	env := map[string]string{"TERMOCODE_SOCKET": "/s/a.sock", "TERMOCODE_TAB_ID": "t9"}
	getenv := func(k string) string { return env[k] }
	run := func(stdin string, args ...string) (int, string, string) {
		var out, errw bytes.Buffer
		code := runAgentIn(args, strings.NewReader(stdin), &out, &errw, getenv)
		return code, out.String(), errw.String()
	}
	if code, out, _ := run("", "list"); code != 0 || !strings.Contains(out, "t1") || !strings.Contains(out, "claude") || !strings.Contains(out, "waiting") || !strings.Contains(out, "zsh *") {
		t.Errorf("list %d %q", code, out)
	}
	if code, out, _ := run("", "list", "--json"); code != 0 || !strings.Contains(out, `"id":"t1"`) {
		t.Errorf("list --json %q", out)
	}
	if code, _, _ := run("", "prompt", "t1", "fix", "the", "tests"); code != 0 || got[len(got)-1].Text != "fix the tests" || got[len(got)-1].Tab != "t1" {
		t.Errorf("prompt %d %+v", code, got[len(got)-1])
	}
	if code, _, _ := run("from stdin\n", "prompt", "t1", "-"); code != 0 || got[len(got)-1].Text != "from stdin\n" {
		t.Errorf("prompt - %+v", got[len(got)-1])
	}
	if code, out, _ := run("", "wait", "t1", "--until", "waiting", "--timeout", "90s"); code != 0 || strings.TrimSpace(out) != "waiting" || got[len(got)-1].TimeoutS != 90 {
		t.Errorf("wait %d %q %+v", code, out, got[len(got)-1])
	}
	if code, _, _ := run("", "wait", "t1", "--until", "idle"); code == 0 {
		t.Error("bad --until accepted")
	}
	if code, _, errs := run("", "report", "waiting", "--agent", "claude"); code != 0 || errs != "" {
		t.Errorf("report must stay quiet and exit 0: %d %q", code, errs)
	}
	if r := got[len(got)-1]; r.Op != agentctl.OpReport || r.Tab != "t9" || r.Agent != "claude" || r.State != "waiting" {
		t.Errorf("report request %+v", r)
	}
	for _, p := range paths {
		if p != "/s/a.sock" {
			t.Errorf("socket %q", p)
		}
	}
	if code, _, _ := run("", "dance"); code != 2 {
		t.Error("unknown subcommand")
	}
	delete(env, "TERMOCODE_TAB_ID")
	if code, _, _ := run("", "report", "done"); code != 1 {
		t.Error("report without a tab must say so")
	}
}
