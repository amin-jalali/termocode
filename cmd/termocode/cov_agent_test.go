package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

func agentCLI(t *testing.T, env map[string]string, call func(agentctl.Request) (agentctl.Reply, error), stdin string, args ...string) (int, string, string) {
	t.Helper()
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	agentCall = func(_ string, r agentctl.Request, _ time.Duration) (agentctl.Reply, error) { return call(r) }
	var out, errw bytes.Buffer
	code := runAgentIn(args, strings.NewReader(stdin), &out, &errw, func(k string) string { return env[k] })
	return code, out.String(), errw.String()
}

func TestAgentCLIUsageAndFailedCalls(t *testing.T) {
	env := map[string]string{"TERMOCODE_SOCKET": "/s", "TERMOCODE_TAB_ID": "t1"}
	ok := func(agentctl.Request) (agentctl.Reply, error) { return agentctl.Reply{OK: true, State: "done"}, nil }
	bad := func(agentctl.Request) (agentctl.Reply, error) { return agentctl.Reply{}, errors.New("not running") }
	for _, a := range [][]string{
		nil, {"prompt"}, {"prompt", "t1"}, {"wait"}, {"wait", "t1", "--bogus"}, {"report"}, {"report", "done", "--bogus"}, {"nonsense"},
	} {
		if code, _, _ := agentCLI(t, env, ok, "", a...); code != 2 {
			t.Errorf("%v: %d", a, code)
		}
	}
	for _, a := range [][]string{{"list"}, {"prompt", "t1", "hi"}, {"wait", "t1"}} {
		if code, _, errw := agentCLI(t, env, bad, "", a...); code != 1 || !strings.Contains(errw, "not running") {
			t.Errorf("%v: %d %q", a, code, errw)
		}
	}
	if code, _, errw := agentCLI(t, env, ok, "", "wait", "t1", "--timeout", "soon"); code != 1 || !strings.Contains(errw, "bad --timeout") {
		t.Errorf("bad timeout: %d %q", code, errw)
	}
	if code, out, _ := agentCLI(t, env, ok, "", "wait", "t1", "--until=done"); code != 0 || strings.TrimSpace(out) != "done" {
		t.Errorf("--until=: %d %q", code, out)
	}
	if code, _, _ := agentCLI(t, env, ok, "", "--help"); code != 0 {
		t.Errorf("help: %d", code)
	}
}

func TestAgentReportForms(t *testing.T) {
	var got []agentctl.Request
	rec := func(r agentctl.Request) (agentctl.Reply, error) {
		got = append(got, r)
		return agentctl.Reply{}, errors.New("gone")
	}
	env := map[string]string{"TERMOCODE_SOCKET": "/s", "TERMOCODE_TAB_ID": "t9", "TERMOCODE_AGENT_DEBUG": "1"}
	code, _, errw := agentCLI(t, env, rec, "", "report", "waiting", "--agent=codex", "--tab", "t2", `{"event":"x"}`)
	if code != 0 || len(got) != 1 || got[0].Agent != "codex" || got[0].Tab != "t2" {
		t.Fatalf("%d %+v", code, got)
	}
	if !strings.Contains(errw, "agent report") {
		t.Errorf("debug text %q", errw)
	}
	// Outside a termocode terminal the hook does nothing.
	got = nil
	if code, _, _ := agentCLI(t, map[string]string{"TERMOCODE_SOCKET": "/s"}, rec, "", "report", "done"); code != 0 || len(got) != 0 {
		t.Errorf("no tab: %d %v", code, got)
	}
	if agentSocket(func(k string) string { return map[string]string{"TERMOCODE_SOCKET": "/x"}[k] }) != "/x" {
		t.Fatal("socket from the environment")
	}
}

func TestAgentPromptStdinFailure(t *testing.T) {
	old := agentCall
	t.Cleanup(func() { agentCall = old })
	var out, errw bytes.Buffer
	if code := runAgentIn([]string{"prompt", "t1", "-"}, errReader{}, &out, &errw, func(string) string { return "" }); code != 1 || !strings.Contains(errw.String(), "read stdin") {
		t.Errorf("%d %q", code, errw.String())
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }
