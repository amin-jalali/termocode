package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// eventually waits (up to 10 s) for ok.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func logCount(path, method string) int {
	b, _ := os.ReadFile(path)
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if l == method {
			n++
		}
	}
	return n
}

// Spec (MC-3): Warnings are the config problems of the last load; the
// caller gets a copy.
func TestManagerWarnings(t *testing.T) {
	e := newManagerEnv(t)
	writeFile(t, e.user, `{"mcpServers": {`)
	m := e.manager(t, 0, nil)
	w := m.Warnings()
	if len(w) == 0 {
		t.Fatal("a broken mcp.json gave no warning")
	}
	w[0] = "changed"
	if m.Warnings()[0] == "changed" {
		t.Fatal("Warnings hands out its own slice")
	}
}

// Spec (MC-3): after Close nothing starts again.
func TestManagerClosedRefusesStarts(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	m.Close()
	if _, err := m.Call(context.Background(), nil, "fake", "echo", nil); err == nil || !strings.Contains(err.Error(), "shut down") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(e.log); err == nil {
		t.Fatal("a server started after Close")
	}
}

// Spec (MC-3): SetTimeouts arms the new idle timeout on running servers
// at once; a call timeout <= 0 keeps the old one.
func TestManagerSetTimeoutsStopsIdleServer(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	if _, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	if m.Running() != 1 {
		t.Fatal("not running")
	}
	m.SetTimeouts(50*time.Millisecond, 0)
	if idle, call := m.Timeouts(); idle != 50*time.Millisecond || call != 5*time.Second {
		t.Fatalf("timeouts %v %v", idle, call)
	}
	eventually(t, "the idle stop", func() bool { return m.Running() == 0 })
	if st := status(m, "fake"); st.State != StateStopped {
		t.Fatalf("state %v", st.State)
	}
	m.SetTimeouts(0, 2*time.Second)
	if _, call := m.Timeouts(); call != 2*time.Second {
		t.Fatalf("call timeout %v", call)
	}
}

// Spec (MC-3): the idle timer never stops a server in the middle of a
// call; it stops it after.
func TestManagerIdleWaitsForRunningCall(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	if _, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	type res struct {
		r   CallResult
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := m.Call(context.Background(), nil, "fake", "slow", map[string]any{"total_ms": 500})
		done <- res{r, err}
	}()
	eventually(t, "the slow call to start", func() bool { return logCount(e.log, "tools/call") == 2 })
	m.SetTimeouts(30*time.Millisecond, 0) // fires while the call runs
	got := <-done
	if got.err != nil || got.r.IsError {
		t.Fatalf("the slow call was cut: %q %v", got.r.Text(), got.err)
	}
	if n := logCount(e.log, "initialize"); n != 1 {
		t.Fatalf("the server started %d times: the idle timer stopped it during the call", n)
	}
	eventually(t, "the idle stop after the call", func() bool { return m.Running() == 0 })
}

// Spec (MC-4): with no cache dir nothing is written; a damaged cache
// file is ignored; KnownTools never starts a server.
func TestManagerCacheEdgeCases(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := NewManager(ManagerOptions{UserPath: e.user, ProjectPath: e.project, CallTimeout: 5 * time.Second,
		Expander: func() Expander { return Expander{Env: os.LookupEnv, Workspace: e.dir} }})
	t.Cleanup(m.Close)
	if n := countTools(m.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("tools %d", n)
	}
	if _, err := os.Stat(e.cache); err == nil {
		t.Fatal("a cache was written with no cache dir")
	}
	if _, _, ok := m.KnownTools("nope"); ok {
		t.Fatal("KnownTools knows an unknown server")
	}

	e2 := newManagerEnv(t)
	e2.write(t, e2.user, map[string]any{"fake": map[string]string{}})
	writeFile(t, filepath.Join(e2.cache, "fake.tools.json"), "{damaged")
	m2 := e2.manager(t, 0, nil)
	if _, tools, have := m2.KnownTools("fake"); have || len(tools) != 0 {
		t.Fatalf("a damaged cache was used: %d tools", len(tools))
	}
	if _, err := os.Stat(e2.log); err == nil {
		t.Fatal("KnownTools started the server")
	}
}

// Spec (MC-2): a server the user denied is skipped on later turns
// without asking again.
func TestManagerDeniedServerSkipped(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.project, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	asked := 0
	ask := func(context.Context, Server) TrustAnswer { asked++; return TrustDeny }
	if n := countTools(m.Tools(context.Background(), ask)); n != 0 || asked != 1 {
		t.Fatalf("first turn: %d tools, asked %d", n, asked)
	}
	if n := countTools(m.Tools(context.Background(), ask)); n != 0 || asked != 1 {
		t.Fatalf("second turn: %d tools, asked %d", n, asked)
	}
	if _, err := os.Stat(e.log); err == nil {
		t.Fatal("a denied server started")
	}
}

// Spec (MC-4): AcceptTools trusts the current (changed) list and saves
// it; an unknown name saves nothing.
func TestManagerAcceptTools(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	tr := &toolsRec{}
	m := e.managerTools(t, tr)
	m.Tools(context.Background(), nil)
	first := tr.get("fake")
	e.write(t, e.user, map[string]any{"fake": map[string]string{"FAKE_TOOLSET": "2"}})
	m.Reload()
	list := m.Tools(context.Background(), nil)
	if len(list) != 1 || !list[0].ToolsChanged {
		t.Fatalf("changed list not marked: %+v", list)
	}
	m.AcceptTools("nope")
	if len(tr.saved) != 1 {
		t.Fatalf("an unknown server was saved: %v", tr.saved)
	}
	m.AcceptTools("fake")
	if tr.get("fake") == first || tr.get("fake") == "" {
		t.Fatal("AcceptTools did not save the new list")
	}
	if list := m.Tools(context.Background(), nil); len(list) != 1 || list[0].ToolsChanged {
		t.Fatalf("after accept: %+v", list)
	}
}

// Spec (MC-3): Caller runs tools through the manager; Connect and Stop
// of an unknown server do nothing harmful.
func TestManagerCallerConnectStop(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	r, err := m.Caller(nil)(context.Background(), "fake", "echo", map[string]any{"text": "via caller"})
	if err != nil || r.Text() != "echo: via caller" {
		t.Fatalf("Caller = %q %v", r.Text(), err)
	}
	if err := m.Connect(context.Background(), nil, "nope"); err == nil || !strings.Contains(err.Error(), "no MCP server") {
		t.Fatalf("Connect unknown: %v", err)
	}
	m.Stop("nope")
	if m.Running() != 1 {
		t.Fatal("Stop of an unknown name stopped something")
	}
}
