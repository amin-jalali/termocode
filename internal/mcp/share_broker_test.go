//go:build unix

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/session"
)

// fakeShareConfig runs the fake server under a broker.
func fakeShareConfig(env map[string]string) shareConfig {
	e := mcptest.Entry(env)
	return shareConfig{Command: e["command"].(string), Args: e["args"].([]string), Env: e["env"].(map[string]string)}
}

// startTestBroker starts a broker in this process on a temp socket.
func startTestBroker(t *testing.T, cfg shareConfig) (*broker, string) {
	t.Helper()
	dir, err := shareDir(&ShareOptions{Dir: filepath.Join(shortTemp(t), "mcp")})
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "test.sock")
	b, err := startBroker(sock, cfg, session.CheckPeer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.shutdown() })
	return b, sock
}

// shortTemp is a temp dir for socket folders (<it>/mcp). Under a long
// $TMPDIR (macOS) the sockets go to a short /tmp folder, removed too.
func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "tcs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(d)
		cleanShortDir(filepath.Join(d, "mcp"))
	})
	return d
}

type shareSeen struct {
	sessions, pid atomic.Int64
	changed       atomic.Int64
	mu            sync.Mutex
	lines         []string
}

// dialTestShare connects one session to the broker and runs its handshake.
func dialTestShare(t *testing.T, sock string, seen *shareSeen) *Client {
	t.Helper()
	nc, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil {
		seen = &shareSeen{}
	}
	c := NewClient(nc, nc, Options{
		Shared:       func(n, pid int) { seen.sessions.Store(int64(n)); seen.pid.Store(int64(pid)) },
		ToolsChanged: func() { seen.changed.Add(1) },
		Log: func(l string) {
			seen.mu.Lock()
			seen.lines = append(seen.lines, l)
			seen.mu.Unlock()
		},
	})
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func countLines(path, line string) int {
	b, _ := os.ReadFile(path)
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if l == line {
			n++
		}
	}
	return n
}

func TestShareBrokerTwoSessionsOneServer(t *testing.T) {
	log := filepath.Join(t.TempDir(), "fake.log")
	b, sock := startTestBroker(t, fakeShareConfig(map[string]string{"FAKE_LOG": log}))
	s1, s2 := &shareSeen{}, &shareSeen{}
	c1 := dialTestShare(t, sock, s1)
	c2 := dialTestShare(t, sock, s2)
	if c1.ServerInfo().Name != "fake-mcp" || c2.ServerInfo().Name != "fake-mcp" {
		t.Fatalf("server info %+v %+v", c1.ServerInfo(), c2.ServerInfo())
	}
	waitFor(t, "2 sessions", func() bool { return s1.sessions.Load() == 2 && s2.sessions.Load() == 2 })
	if int(s1.pid.Load()) != b.pid() || b.pid() == 0 {
		t.Fatalf("pid %d, want %d", s1.pid.Load(), b.pid())
	}
	var wg sync.WaitGroup
	for i, c := range []*Client{c1, c2, c1, c2} {
		wg.Add(1)
		go func(i int, c *Client) {
			defer wg.Done()
			want := "echo: " + string(rune('a'+i))
			res, err := c.CallTool(context.Background(), "echo", map[string]any{"text": string(rune('a' + i))})
			if err != nil || res.Text() != want {
				t.Errorf("call %d: %q %v, want %q", i, res.Text(), err, want)
			}
		}(i, c)
	}
	wg.Wait()
	if tools, err := c2.ListTools(context.Background()); err != nil || len(tools) == 0 {
		t.Fatalf("tools/list: %d %v", len(tools), err)
	}
	if n := countLines(log, "initialize"); n != 1 {
		t.Fatalf("the server was initialized %d times, want once", n)
	}
	c1.Close()
	waitFor(t, "1 session", func() bool { return s2.sessions.Load() == 1 })
}

func TestShareBrokerFansOutToolsChanged(t *testing.T) {
	_, sock := startTestBroker(t, fakeShareConfig(nil))
	s1, s2 := &shareSeen{}, &shareSeen{}
	c1 := dialTestShare(t, sock, s1)
	dialTestShare(t, sock, s2)
	if _, err := c1.CallTool(context.Background(), "notify_changed", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "list_changed in both sessions", func() bool { return s1.changed.Load() == 1 && s2.changed.Load() == 1 })
}

func TestShareBrokerProgressGoesToTheCaller(t *testing.T) {
	_, sock := startTestBroker(t, fakeShareConfig(nil))
	c1 := dialTestShare(t, sock, nil)
	c2 := dialTestShare(t, sock, nil)
	// Both sessions use the same token ("termocode-1"); the 800 ms idle
	// deadline only holds when each one gets its own progress (a 50 ms
	// beat). The call runs 2.4 s, so without progress it times out; the
	// wide gap keeps a slow macOS runner from missing one beat window.
	var wg sync.WaitGroup
	for _, c := range []*Client{c1, c2} {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			res, err := c.CallToolTimeout(context.Background(), "slow", map[string]any{"total_ms": 2400, "progress_ms": 50}, 800*time.Millisecond)
			if err != nil || res.Text() != "slow done" {
				t.Errorf("slow: %q %v", res.Text(), err)
			}
		}(c)
	}
	wg.Wait()
}

func TestShareBrokerCancelReachesServer(t *testing.T) {
	log := filepath.Join(t.TempDir(), "fake.log")
	_, sock := startTestBroker(t, fakeShareConfig(map[string]string{"FAKE_LOG": log}))
	c := dialTestShare(t, sock, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := c.CallTool(ctx, "slow", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	waitFor(t, "the server's cancel", func() bool {
		b, _ := os.ReadFile(log)
		return strings.Contains(string(b), "cancelled ")
	})
}

func TestShareBrokerAnswersRoots(t *testing.T) {
	cfg := fakeShareConfig(nil)
	cfg.Roots = []string{"/work/a"}
	_, sock := startTestBroker(t, cfg)
	c := dialTestShare(t, sock, nil)
	res, err := c.CallTool(context.Background(), "roots", nil)
	if err != nil || res.Text() != "roots: file:///work/a" {
		t.Fatalf("roots: %q %v", res.Text(), err)
	}
}

func TestShareBrokerStopsAfterGrace(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "running")
	cfg := fakeShareConfig(map[string]string{"FAKE_MARKER": marker})
	cfg.GraceMS = 150
	b, sock := startTestBroker(t, cfg)
	c := dialTestShare(t, sock, nil)
	time.Sleep(300 * time.Millisecond) // longer than the grace: a session holds it
	select {
	case <-b.done:
		t.Fatal("stopped while a session used it")
	default:
	}
	c.Close()
	select {
	case <-b.done:
	case <-time.After(10 * time.Second):
		t.Fatal("still running after the grace")
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket left: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("server left running: %v", err)
	}
}

func TestShareBrokerStopForAll(t *testing.T) {
	b, sock := startTestBroker(t, fakeShareConfig(nil))
	c1 := dialTestShare(t, sock, nil)
	c2 := dialTestShare(t, sock, nil)
	// The answer to stop is the connection ending, never an error reply.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c1.call(ctx, shareStop, nil); err == nil || ctx.Err() != nil ||
		!strings.Contains(err.Error(), "closed the connection") {
		t.Fatalf("stop: %v", err)
	}
	for _, c := range []*Client{c1, c2} {
		select {
		case <-c.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("a session stayed connected")
		}
	}
	<-b.done
}

func TestShareBrokerServerExitEndsSessions(t *testing.T) {
	b, sock := startTestBroker(t, fakeShareConfig(nil))
	c := dialTestShare(t, sock, nil)
	killGroup(b.pid())
	select {
	case <-c.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the session did not see the server exit")
	}
}

func TestShareBrokerRefusesOtherPeers(t *testing.T) {
	dir, _ := shareDir(&ShareOptions{Dir: filepath.Join(shortTemp(t), "mcp")})
	sock := filepath.Join(dir, "x.sock")
	b, err := startBroker(sock, fakeShareConfig(nil), func(net.Conn) error { return errors.New("other uid") })
	if err != nil {
		t.Fatal(err)
	}
	defer b.shutdown()
	nc, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(nc, nc, Options{})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Initialize(ctx); err == nil {
		t.Fatal("a refused peer got an answer")
	}
	fi, _ := os.Stat(sock)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %o", fi.Mode().Perm())
	}
}

func TestShareBrokerInitFailureLeavesNothing(t *testing.T) {
	dir, _ := shareDir(&ShareOptions{Dir: filepath.Join(shortTemp(t), "mcp")})
	sock := filepath.Join(dir, "x.sock")
	_, err := startBroker(sock, fakeShareConfig(map[string]string{"FAKE_VERSION": "1999-01-01"}), session.CheckPeer)
	if err == nil || !strings.Contains(err.Error(), "1999-01-01") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket left: %v", err)
	}
	_, err = startBroker(sock, shareConfig{Command: "termocode-no-such-server"}, session.CheckPeer)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err %v", err)
	}
}

func TestShareProgressTokenRoundTrip(t *testing.T) {
	in := json.RawMessage(`{"name":"slow","_meta":{"progressToken":"termocode-1"}}`)
	out := rewriteProgress(in, 7)
	var p struct {
		Meta struct {
			Token string `json:"progressToken"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(out, &p)
	if p.Meta.Token != `c7:"termocode-1"` {
		t.Fatalf("token %q", p.Meta.Token)
	}
	id, back, ok := routeProgress(json.RawMessage(`{"progressToken":` + mustJSON(p.Meta.Token) + `,"progress":3}`))
	if !ok || id != 7 || !strings.Contains(string(back), `"progressToken":"termocode-1"`) {
		t.Fatalf("route %d %s %v", id, back, ok)
	}
	if _, _, ok := routeProgress(json.RawMessage(`{"progressToken":"other"}`)); ok {
		t.Fatal("a foreign token was routed")
	}
	plain := json.RawMessage(`{"name":"echo"}`)
	if string(rewriteProgress(plain, 1)) != string(plain) {
		t.Fatal("params without a token changed")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Spec: a broker that can not listen says "unavailable" (the session
// runs its own server), not a server error.
func TestShareBrokerListenFailureIsUnavailable(t *testing.T) {
	var out strings.Builder
	sock := filepath.Join(shortTemp(t), "no-such-folder", "x.sock")
	in := strings.NewReader(`{"command":"` + os.Args[0] + `"}`)
	if code := runShareBroker(sock, in, &out); code == 0 || !strings.HasPrefix(out.String(), "unavailable: ") {
		t.Fatalf("exit %d, said %q", code, out.String())
	}
}

// Spec: the last session that leaves on purpose (idle stop, quit) stops
// the server at once; the grace is only for a session that dropped.
func TestShareBrokerLastLeaveStopsAtOnce(t *testing.T) {
	cfg := fakeShareConfig(nil)
	cfg.GraceMS = 60_000
	b, sock := startTestBroker(t, cfg)
	c1 := dialTestShare(t, sock, nil)
	c2 := dialTestShare(t, sock, nil)
	p1 := &Process{Client: c1, shared: true, done: make(chan struct{})}
	p1.closeFn = func() { leaveShared(c1) }
	go func() { <-c1.Done(); close(p1.done) }()
	p1.Close()
	select {
	case <-b.done:
		t.Fatal("stopped while another session used it")
	case <-time.After(200 * time.Millisecond):
	}
	leaveShared(c2)
	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the last session left, the server still runs")
	}
}

func TestShareBrokerDroppedSessionKeepsGrace(t *testing.T) {
	cfg := fakeShareConfig(nil)
	cfg.GraceMS = 60_000
	b, sock := startTestBroker(t, cfg)
	c := dialTestShare(t, sock, nil)
	c.Close() // no leave: a crash
	select {
	case <-b.done:
		t.Fatal("a dropped session stopped it without the grace")
	case <-time.After(500 * time.Millisecond):
	}
}

// Spec (MCPX-20): the broker initialized the server once, the old way;
// a session's server/discover probe gets "not supported", so every
// session uses that same handshake even when the server is dual-era.
func TestShareBrokerSessionsStayOnTheHandshake(t *testing.T) {
	_, sock := startTestBroker(t, fakeShareConfig(map[string]string{"FAKE_MODERN": "1"}))
	c := dialTestShare(t, sock, nil)
	if c.Stateless() || c.Version() != ProtocolVersion {
		t.Fatalf("stateless %v version %q, want the broker's handshake", c.Stateless(), c.Version())
	}
}
