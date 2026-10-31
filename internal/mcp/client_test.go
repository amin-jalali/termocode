package mcp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// TestMain lets this test binary also be the fake MCP server (mcptest).
func TestMain(m *testing.M) {
	RunShareIfHelper() // the share broker re-runs this binary
	mcptest.RunIfHelper()
	os.Exit(m.Run())
}

// fakeServer is a Server entry that runs the fake with env.
func fakeServer(t *testing.T, env map[string]string) Server {
	t.Helper()
	e := mcptest.Entry(env)
	s := Server{Name: "fake", Type: TypeStdio, Command: e["command"].(string), Args: e["args"].([]string),
		Env: e["env"].(map[string]string), Source: SourceUser}
	return s
}

// logLines collects Options.Log lines.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }
func (l *logLines) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func startFake(t *testing.T, env map[string]string, opts Options) *Process {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, err := StartStdio(ctx, fakeServer(t, env), opts)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// fakeLog reads the methods the fake received (FAKE_LOG).
func fakeLog(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

func pidAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	st := strings.TrimSpace(string(out))
	return err == nil && st != "" && !strings.HasPrefix(st, "Z")
}

func waitGone(t *testing.T, pid int, d time.Duration) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if !pidAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("process %d still running after %v", pid, d)
}

// Handshake: initialize with our newest version, the server's info, then
// notifications/initialized. A banner line on stdout is tolerated and
// logged.
func TestHandshake(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	var ll logLines
	p := startFake(t, map[string]string{"FAKE_LOG": log, "FAKE_BANNER": "1"}, Options{Log: ll.add})
	if v := p.Client.Version(); v != ProtocolVersion {
		t.Errorf("version %q, want %q", v, ProtocolVersion)
	}
	if info := p.Client.ServerInfo(); info.Name != "fake-mcp" || info.Version != "1.0.0" {
		t.Errorf("server info %+v", info)
	}
	if _, err := p.Client.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fakeLog(t, log); !strings.HasPrefix(got, "initialize\nnotifications/initialized\n") {
		t.Errorf("fake got %q", got)
	}
	if !ll.has("fake-mcp v1 starting") {
		t.Errorf("banner line not logged: %q", ll.lines)
	}
}

// An older version the server answers is accepted; an unknown one fails.
func TestVersionNegotiation(t *testing.T) {
	p := startFake(t, map[string]string{"FAKE_VERSION": "2025-03-26"}, Options{})
	if v := p.Client.Version(); v != "2025-03-26" {
		t.Errorf("version %q", v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q, err := StartStdio(ctx, fakeServer(t, map[string]string{"FAKE_VERSION": "1999-01-01"}), Options{})
	if err == nil {
		q.Close()
		t.Fatal("unknown version accepted")
	}
	if !strings.Contains(err.Error(), "1999-01-01") {
		t.Errorf("err = %v", err)
	}
}

// tools/list follows nextCursor; annotations and the full schema arrive.
func TestListToolsPages(t *testing.T) {
	p := startFake(t, nil, Options{})
	tools, err := p.Client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range tools {
		names = append(names, x.Name)
	}
	want := "echo write_note delete_all slow big image fails roots mixed structured"
	if strings.Join(names, " ") != want {
		t.Fatalf("tools %q, want %q", strings.Join(names, " "), want)
	}
	if a := tools[0].Annotations; a == nil || a.ReadOnlyHint == nil || !*a.ReadOnlyHint || tools[0].Title != "Echo" {
		t.Errorf("echo = %+v", tools[0])
	}
	if a := tools[1].Annotations; a == nil || a.DestructiveHint == nil || *a.DestructiveHint {
		t.Errorf("write_note annotations %+v", a)
	}
	if tools[2].Annotations != nil {
		t.Errorf("delete_all has annotations %+v", tools[2].Annotations)
	}
	if _, ok := tools[9].InputSchema["$defs"]; !ok {
		t.Errorf("structured schema lost $defs: %v", tools[9].InputSchema)
	}
}

// tools/call results become text for the model.
func TestCallResultText(t *testing.T) {
	p := startFake(t, nil, Options{Roots: func() []string { return []string{"/work/a b"} }})
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"echo", map[string]any{"text": "hi"}, "echo: hi"},
		{"fails", nil, "error: it broke"},
		{"image", nil, "[image image/png, 3 KB — not shown]"},
		{"mixed", nil, "first\nfile body\n[resource file:///b.bin, application/octet-stream]\n[resource link c: file:///c.txt]"},
		{"structured", nil, `{"n":42}`},
		{"roots", nil, "roots: file:///work/a%20b"},
	}
	for _, c := range cases {
		r, err := p.Client.CallTool(context.Background(), c.tool, c.args)
		if err != nil {
			t.Errorf("%s: %v", c.tool, err)
			continue
		}
		if got := r.Text(); got != c.want {
			t.Errorf("%s = %q, want %q", c.tool, got, c.want)
		}
	}
	// An unknown tool is a JSON-RPC error.
	if _, err := p.Client.CallTool(context.Background(), "nope", nil); err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unknown tool: %v", err)
	}
}

// Cancelling a call returns at once and tells the server
// (notifications/cancelled with the request id).
func TestCallCancel(t *testing.T) {
	log := filepath.Join(t.TempDir(), "log")
	p := startFake(t, map[string]string{"FAKE_LOG": log}, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := p.Client.CallTool(ctx, "slow", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("cancel took %v", d)
	}
	end := time.Now().Add(3 * time.Second)
	for !strings.Contains(fakeLog(t, log), "cancelled ") && time.Now().Before(end) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(fakeLog(t, log), "cancelled ") {
		t.Errorf("server never got notifications/cancelled: %q", fakeLog(t, log))
	}
	// The connection still works.
	if r, err := p.Client.CallTool(context.Background(), "echo", map[string]any{"text": "again"}); err != nil || r.Text() != "echo: again" {
		t.Errorf("after cancel: %q %v", r.Text(), err)
	}
}

// Close kills the whole process group, even a server that ignores the
// end of its input.
func TestCloseKillsServer(t *testing.T) {
	p := startFake(t, map[string]string{"FAKE_IGNORE_SHUTDOWN": "1"}, Options{})
	pid := p.Pid()
	if !pidAlive(pid) {
		t.Fatalf("pid %d not running", pid)
	}
	start := time.Now()
	p.Close()
	waitGone(t, pid, 5*time.Second)
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("close took %v", d)
	}
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Error("Done not closed after Close")
	}
}

// The child gets a minimal environment: the entry's env and basics like
// PATH, never termocode's API keys.
func TestMinimalEnv(t *testing.T) {
	env := childEnv([]string{"PATH=/bin", "HOME=/h", "ANTHROPIC_API_KEY=sk-ant-x", "OPENAI_API_KEY=y",
		"TERMOCODE_AI_API_KEY=z", "LANG=C.UTF-8", "XDG_CONFIG_HOME=/c", "LC_ALL=C"}, map[string]string{"TOKEN": "t", "PATH": "/custom"})
	got := strings.Join(env, " ")
	for _, want := range []string{"HOME=/h", "LANG=C.UTF-8", "XDG_CONFIG_HOME=/c", "LC_ALL=C", "TOKEN=t", "PATH=/custom"} {
		if !strings.Contains(got, want) {
			t.Errorf("env misses %s: %q", want, got)
		}
	}
	for _, bad := range []string{"ANTHROPIC", "OPENAI", "TERMOCODE_AI", "PATH=/bin"} {
		if strings.Contains(got, bad) {
			t.Errorf("env leaks %s: %q", bad, got)
		}
	}
}

// A command that does not exist fails with a clear error.
func TestStartMissingCommand(t *testing.T) {
	_, err := StartStdio(context.Background(), Server{Name: "x", Type: TypeStdio, Command: "termocode-no-such-mcp-server"}, Options{})
	if err == nil || !strings.Contains(err.Error(), "termocode-no-such-mcp-server") {
		t.Errorf("err = %v", err)
	}
}
