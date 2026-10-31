package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// Spec (RT-6): the reconnect delay doubles per try, never passes the
// cap, and is jittered into [d/2, d] so many sessions do not knock at
// the same moment.
func TestBackoffDoublesWithJitterAndCap(t *testing.T) {
	base, limit := time.Second, 30*time.Second
	low := func(int64) int64 { return 0 }
	high := func(n int64) int64 { return n - 1 }
	for try, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		if got := Backoff(try, base, limit, high); got < want-time.Nanosecond || got > want {
			t.Errorf("try %d high: %v, want ~%v", try, got, want)
		}
		if got := Backoff(try, base, limit, low); got != want/2 {
			t.Errorf("try %d low: %v, want %v", try, got, want/2)
		}
	}
	if got := Backoff(200, base, limit, high); got > limit || got <= 0 {
		t.Errorf("huge try: %v", got)
	}
	if got := Backoff(3, base, limit, nil); got < 4*time.Second || got > 8*time.Second {
		t.Errorf("default random: %v", got)
	}
}

// flakyNet is an HTTP transport whose network can be cut (dial refused).
type flakyNet struct {
	mu   sync.Mutex
	down bool
	rt   http.RoundTripper
}

func (f *flakyNet) set(down bool) { f.mu.Lock(); f.down = down; f.mu.Unlock() }

func (f *flakyNet) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	down := f.down
	f.mu.Unlock()
	if down {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	}
	return f.rt.RoundTrip(r)
}

func reconnectManager(t *testing.T, e *managerEnv, fn *flakyNet, tokens TokenStore) *Manager {
	t.Helper()
	m := NewManager(ManagerOptions{
		UserPath: e.user, ProjectPath: e.project, CacheDir: e.cache,
		Expander:      func() Expander { return Expander{Workspace: e.dir} },
		CallTimeout:   5 * time.Second,
		HTTP:          &http.Client{Transport: fn},
		Tokens:        tokens,
		ReconnectBase: 20 * time.Millisecond, ReconnectCap: 40 * time.Millisecond,
	})
	t.Cleanup(m.Close)
	return m
}

// Spec (RT-6): a remote server that can not be reached is "reconnecting
// in Ns", tries again by itself and is running once the network is back.
func TestRemoteServerReconnectsByItself(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"rem": map[string]any{"type": "http", "url": f.MCPURL()}})
	fn := &flakyNet{rt: http.DefaultTransport, down: true}
	m := reconnectManager(t, e, fn, nil)
	m.opts.ReconnectBase, m.opts.ReconnectCap = time.Hour, time.Hour // first: hold the timer
	err := m.Connect(context.Background(), nil, "rem")
	var ce *ConnError
	if !errors.As(err, &ce) {
		t.Fatalf("connect offline: %v", err)
	}
	st := status(m, "rem")
	if st.State != StateReconnecting || !strings.HasPrefix(st.StateText(), "reconnecting in ") || st.RetryIn <= 0 {
		t.Fatalf("status %+v %q", st, st.StateText())
	}
	// The network is back: a Connect by the user works and ends the timer.
	fn.set(false)
	if err := m.Connect(context.Background(), nil, "rem"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	timer := m.servers["rem"].retryTimer
	m.mu.Unlock()
	if st := status(m, "rem"); st.State != StateRunning || timer != nil || m.retries("rem") != 0 {
		t.Errorf("after connect %+v timer %v", st, timer != nil)
	}
}

// Spec (RT-6): the timer runs the reconnect without any call.
func TestRemoteServerReconnectTimer(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"rem": map[string]any{"type": "http", "url": f.MCPURL()}})
	fn := &flakyNet{rt: http.DefaultTransport, down: true}
	m := reconnectManager(t, e, fn, nil)
	_ = m.Connect(context.Background(), nil, "rem")
	eventually(t, "two tries", func() bool { return countPosts(f) == 0 && m.retries("rem") >= 2 })
	fn.set(false)
	eventually(t, "running", func() bool { return status(m, "rem").State == StateRunning })
	if n := m.retries("rem"); n != 0 {
		t.Errorf("tries not reset: %d", n)
	}
}

// Spec (RT-6): an auth answer stops the reconnects (sign in first).
func TestRemoteReconnectStopsOnAuth(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "x"})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"rem": map[string]any{"type": "http", "url": f.MCPURL()}})
	fn := &flakyNet{rt: http.DefaultTransport}
	m := reconnectManager(t, e, fn, &memTokens{})
	_ = m.Connect(context.Background(), nil, "rem")
	if st := status(m, "rem"); st.State != StateSignIn || m.retries("rem") != 0 {
		t.Fatalf("status %+v tries %d", st, m.retries("rem"))
	}
}

// Spec (RT-6): after ReconnectMax tries it gives up: failed, no timer.
func TestRemoteReconnectGivesUp(t *testing.T) {
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"rem": map[string]any{"type": "http", "url": "https://example.invalid/mcp"}})
	fn := &flakyNet{rt: http.DefaultTransport, down: true}
	m := reconnectManager(t, e, fn, nil)
	m.opts.ReconnectMax = 2
	_ = m.Connect(context.Background(), nil, "rem")
	eventually(t, "failed", func() bool { return status(m, "rem").State == StateFailed })
	if st := status(m, "rem"); !strings.Contains(st.Problem, "Reconnect All") {
		t.Errorf("problem %q", st.Problem)
	}
	m.mu.Lock()
	timer := m.servers["rem"].retryTimer
	m.mu.Unlock()
	if timer != nil {
		t.Error("a timer is left")
	}
}

func countPosts(f *mcptest.HTTPServer) int { return len(f.Requests()) }

// Spec (RT-6): a call that loses the network drops the connection and
// starts the reconnects (the call itself is not run again).
func TestRemoteCallDropStartsReconnect(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{"rem": map[string]any{"type": "http", "url": f.MCPURL()}})
	fn := &flakyNet{rt: http.DefaultTransport}
	m := reconnectManager(t, e, fn, nil)
	m.opts.ReconnectBase, m.opts.ReconnectCap = time.Hour, time.Hour
	if err := m.Connect(context.Background(), nil, "rem"); err != nil {
		t.Fatal(err)
	}
	fn.set(true)
	if _, err := m.Call(context.Background(), nil, "rem", "echo", map[string]any{"text": "a"}); err == nil {
		t.Fatal("call worked offline")
	}
	if st := status(m, "rem"); st.State != StateReconnecting {
		t.Errorf("status %+v", st)
	}
}

func (m *Manager) retries(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.servers[name].retryN
}

// Spec (RT-5, RT-9): Reconnect all tries every failed or waiting server
// now, and checks the running ones with a ping.
func TestReconnectAll(t *testing.T) {
	f := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{})
	e := newManagerEnv(t)
	e.write(t, e.user, map[string]any{
		"rem": map[string]any{"type": "http", "url": f.MCPURL()},
		"run": map[string]any{"type": "http", "url": f.MCPURL() + "?b=1"},
		"off": map[string]any{"type": "http", "url": f.MCPURL(), "disabled": true},
	})
	fn := &flakyNet{rt: http.DefaultTransport}
	m := reconnectManager(t, e, fn, nil)
	m.opts.ReconnectBase, m.opts.ReconnectCap = time.Hour, time.Hour
	if err := m.Connect(context.Background(), nil, "run"); err != nil {
		t.Fatal(err)
	}
	fn.set(true)
	_ = m.Connect(context.Background(), nil, "rem")
	if r := m.ReconnectAll(context.Background()); r.Tried != 2 || r.OK != 0 || len(r.Failed) != 2 {
		t.Fatalf("offline %+v", r)
	}
	if st := status(m, "run"); st.State != StateReconnecting {
		t.Errorf("the dead running one: %+v", st)
	}
	fn.set(false)
	if r := m.ReconnectAll(context.Background()); r.Tried != 2 || r.OK != 2 || len(r.Failed) != 0 {
		t.Fatalf("online %+v", r)
	}
	for _, n := range []string{"rem", "run"} {
		if st := status(m, n); st.State != StateRunning {
			t.Errorf("%s: %+v", n, st)
		}
	}
	if st := status(m, "off"); st.State != StateDisabled {
		t.Errorf("off: %+v", st)
	}
	// All running and healthy: one ping each, nothing else.
	if r := m.ReconnectAll(context.Background()); r.Tried != 0 || r.Checked != 2 {
		t.Errorf("healthy %+v", r)
	}
}
