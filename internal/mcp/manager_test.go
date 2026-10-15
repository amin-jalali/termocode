package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// managerEnv is a temp config: user / project mcp.json, cache dir.
type managerEnv struct {
	dir, user, project, cache, log string
}

func newManagerEnv(t *testing.T) *managerEnv {
	t.Helper()
	d := t.TempDir()
	return &managerEnv{dir: d, user: filepath.Join(d, "cfg", "mcp.json"),
		project: filepath.Join(d, "root", ".termocode", "mcp.json"), cache: filepath.Join(d, "cache"),
		log: filepath.Join(d, "fake.log")}
}

// write puts servers (name → extra env, or a raw entry) into the file.
func (e *managerEnv) write(t *testing.T, path string, servers map[string]any) {
	t.Helper()
	m := map[string]any{}
	for name, v := range servers {
		switch v := v.(type) {
		case map[string]string:
			env := map[string]string{"FAKE_LOG": e.log}
			for k, x := range v {
				env[k] = x
			}
			m[name] = mcptest.Entry(env)
		default:
			m[name] = v
		}
	}
	b, _ := json.Marshal(map[string]any{"mcpServers": m})
	writeFile(t, path, string(b))
}

type trustRec struct {
	mu    sync.Mutex
	saved map[string]string
}

func (r *trustRec) isTrusted(s Server) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saved[s.Name] == s.Hash()
}

func (r *trustRec) save(s Server) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saved == nil {
		r.saved = map[string]string{}
	}
	r.saved[s.Name] = s.Hash()
}

func (e *managerEnv) manager(t *testing.T, idle time.Duration, tr *trustRec) *Manager {
	t.Helper()
	if tr == nil {
		tr = &trustRec{}
	}
	m := NewManager(ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:    func() Expander { return Expander{Env: os.LookupEnv, Workspace: e.dir} },
		IdleTimeout: idle, CallTimeout: 5 * time.Second,
		IsTrusted: tr.isTrusted, SaveTrust: tr.save,
	})
	t.Cleanup(m.Close)
	return m
}

func status(m *Manager, name string) Status {
	for _, s := range m.Servers() {
		if s.Name == name {
			return s
		}
	}
	return Status{}
}

func countTools(list []ServerTools) int {
	n := 0
	for _, st := range list {
		n += len(st.Tools)
	}
	return n
}

// Nothing runs until tools are needed; the first Tools() starts the
// server once, lists and caches; the idle timer stops it again. A new
// Manager uses the disk cache and starts nothing.
func TestManagerLazyStartCacheIdle(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 300*time.Millisecond, nil)
	if st := status(m, "fake"); st.State != StateStopped || st.Pid != 0 {
		t.Fatalf("before use: %+v", st)
	}
	if _, err := os.Stat(e.log); err == nil {
		t.Fatal("server started before it was needed")
	}
	list := m.Tools(context.Background(), nil)
	if countTools(list) != 10 || list[0].Server.Name != "fake" {
		t.Fatalf("tools: %d", countTools(list))
	}
	st := status(m, "fake")
	if st.State != StateRunning || st.Pid == 0 || st.Tools != 10 {
		t.Fatalf("after list: %+v", st)
	}
	waitGone(t, st.Pid, 5*time.Second)
	end := time.Now().Add(2 * time.Second)
	for status(m, "fake").State != StateStopped && time.Now().Before(end) {
		time.Sleep(20 * time.Millisecond)
	}
	if s := status(m, "fake"); s.State != StateStopped || s.Tools != 10 {
		t.Errorf("after idle: %+v", s)
	}

	before, _ := os.ReadFile(e.log)
	m2 := e.manager(t, 0, nil)
	if n := countTools(m2.Tools(context.Background(), nil)); n != 10 {
		t.Fatalf("cached tools: %d", n)
	}
	after, _ := os.ReadFile(e.log)
	if string(after) != string(before) || m2.Running() != 0 {
		t.Errorf("cached list started the server: log grew %q", strings.TrimPrefix(string(after), string(before)))
	}
	if !status(m2, "fake").Cached {
		t.Error("status does not say cached")
	}
}

// A call starts the server (lazy), runs, and resets the idle timer.
func TestManagerCall(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]string{}})
	m := e.manager(t, 0, nil)
	r, err := m.Call(context.Background(), nil, "fake", "echo", map[string]any{"text": "hi"})
	if err != nil || r.Text() != "echo: hi" {
		t.Fatalf("call = %q, %v", r.Text(), err)
	}
	if m.Running() != 1 {
		t.Errorf("running = %d", m.Running())
	}
	r, err = m.Call(context.Background(), nil, "fake", "write_note", map[string]any{"title": "two"})
	if err != nil || r.Text() != "wrote two" {
		t.Fatalf("second call = %q, %v", r.Text(), err)
	}
	pid := status(m, "fake").Pid
	m.Stop("fake")
	waitGone(t, pid, 5*time.Second)
	if _, err := m.Call(context.Background(), nil, "nope", "echo", nil); err == nil {
		t.Error("unknown server: no error")
	}
}

// A call slower than the timeout fails with a clear message.
func TestManagerCallTimeout(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"fake": map[string]any{"command": os.Args[0], "args": []string{"-test.run=^$"},
		"env": map[string]string{mcptest.Env: "server"}, "timeout": 1}})
	m := e.manager(t, 0, nil)
	start := time.Now()
	_, err := m.Call(context.Background(), nil, "fake", "slow", nil)
	if err == nil || !strings.Contains(err.Error(), "no answer in 1s") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("timeout took %v", d)
	}
}

// Project servers are never trusted by themselves: Deny keeps them off
// (and is remembered for the session), Once trusts for this session,
// Always is saved.
func TestManagerTrust(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.project, map[string]any{"proj": map[string]string{}})
	var asked []string
	answer := TrustDeny
	ask := func(ctx context.Context, s Server) TrustAnswer {
		asked = append(asked, s.Name+"/"+s.Source)
		return answer
	}
	tr := &trustRec{}
	m := e.manager(t, 0, tr)
	if n := countTools(m.Tools(context.Background(), ask)); n != 0 {
		t.Errorf("denied server gave %d tools", n)
	}
	_, err := m.Call(context.Background(), ask, "proj", "echo", map[string]any{"text": "x"})
	if !errors.Is(err, ErrNotTrusted) {
		t.Errorf("call after deny: %v", err)
	}
	if len(asked) != 1 || asked[0] != "proj/project" {
		t.Errorf("asked %v (deny must be remembered)", asked)
	}
	if _, err := os.Stat(e.log); err == nil {
		t.Error("a denied server was started")
	}

	answer = TrustOnce
	m2 := e.manager(t, 0, tr)
	if n := countTools(m2.Tools(context.Background(), ask)); n != 10 {
		t.Errorf("trusted once: %d tools", n)
	}
	if len(tr.saved) != 0 {
		t.Error("Once was saved")
	}

	answer = TrustAlways
	m3 := e.manager(t, 0, tr)
	if _, err := m3.Call(context.Background(), ask, "proj", "echo", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	if !tr.isTrusted(status(m3, "proj").server) {
		t.Error("Always not saved")
	}
	asked = nil
	m4 := e.manager(t, 0, tr)
	if _, err := m4.Call(context.Background(), ask, "proj", "echo", map[string]any{"text": "x"}); err != nil || len(asked) != 0 {
		t.Errorf("saved trust asked again: %v %v", asked, err)
	}
	// A changed entry asks again.
	e.write(t, e.project, map[string]any{"proj": map[string]string{"X": "1"}})
	m4.Reload()
	answer = TrustDeny
	if _, err := m4.Call(context.Background(), ask, "proj", "echo", map[string]any{"text": "x"}); !errors.Is(err, ErrNotTrusted) || len(asked) != 1 {
		t.Errorf("changed entry: asked %v, err %v", asked, err)
	}
}

// Entries that can not run say why and never start.
func TestManagerProblems(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{
		"needs":  map[string]any{"command": "x", "args": []string{"${TERMOCODE_TEST_UNSET_VAR}"}},
		"remote": map[string]any{"type": "http", "url": "https://mcp.example.com/mcp"},
		"bad":    map[string]any{"command": "x", "url": "https://a"},
		"gone":   map[string]any{"command": "termocode-no-such-mcp-server"},
	})
	m := e.manager(t, 0, nil)
	if n := countTools(m.Tools(context.Background(), nil)); n != 0 {
		t.Errorf("%d tools", n)
	}
	want := map[string]string{
		"needs":  "needs ${TERMOCODE_TEST_UNSET_VAR}",
		"remote": "remote (http) servers are not supported yet",
		"bad":    "url and command both set",
		"gone":   "termocode-no-such-mcp-server not found",
	}
	for name, w := range want {
		st := status(m, name)
		if !strings.Contains(st.Problem, w) {
			t.Errorf("%s: %+v, want problem %q", name, st, w)
		}
		if _, err := m.Call(context.Background(), nil, name, "echo", nil); err == nil || !strings.Contains(err.Error(), w) {
			t.Errorf("%s call: %v", name, err)
		}
	}
	if status(m, "gone").State != StateFailed {
		t.Errorf("gone state %q", status(m, "gone").State)
	}
	m.Stop("bad") // Stop never hides why an entry is invalid
	if st := status(m, "bad"); st.State != StateInvalid || st.Problem == "" {
		t.Errorf("bad after Stop: %+v", st)
	}
}

// Config warnings (a literal token) go to the log once, not on every
// reload.
func TestManagerLogsWarningsOnce(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"gh": map[string]any{"command": "x", "env": map[string]string{"TOKEN": "ghp_abcdefghijklmnopqrstuvwxyz0123"}}})
	var ll logLines
	m := NewManager(ManagerOptions{UserPath: e.user, Log: func(s, l string) { ll.add(s + ": " + l) }})
	t.Cleanup(m.Close)
	m.Reload()
	m.Reload()
	n := 0
	for _, l := range ll.lines {
		if strings.Contains(l, "literal secret") {
			n++
		}
	}
	if n != 1 || !strings.HasPrefix(ll.lines[0], "config: ") {
		t.Errorf("warning logged %d times: %q", n, ll.lines)
	}
}

// Reload stops a server whose entry changed; Close stops everything.
func TestManagerReloadAndClose(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"a": map[string]string{}, "b": map[string]string{}})
	m := e.manager(t, 0, nil)
	for _, n := range []string{"a", "b"} {
		if _, err := m.Call(context.Background(), nil, n, "echo", map[string]any{"text": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	pa, pb := status(m, "a").Pid, status(m, "b").Pid
	e.write(t, e.user, map[string]any{"a": map[string]string{"CHANGED": "1"}, "b": map[string]string{}})
	m.Reload()
	waitGone(t, pa, 5*time.Second)
	if !pidAlive(pb) || status(m, "b").Pid != pb {
		t.Error("unchanged server b was stopped")
	}
	if status(m, "a").Tools != 0 {
		t.Error("changed entry kept its old tools")
	}
	m.Close()
	waitGone(t, pb, 5*time.Second)
}
