//go:build unix

package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// mcpServeShortRuntime points the session dir at a short private temp dir
// (never the user's real one; short for macOS's sun_path) and returns
// the socket path the workspace will use.
func mcpServeShortRuntime(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	path, err := mcpServeSocketPath(aiWorkspaceRoot())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// mcpServeClientLine sends one JSON-RPC line and reads the answer while
// playing Update for the hub.
func mcpServeClientLine(t *testing.T, m *Model, c net.Conn, r *bufio.Reader, line string) map[string]any {
	t.Helper()
	if _, err := c.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
	type res struct {
		s   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
		s, err := r.ReadString('\n')
		ch <- res{s, err}
	}()
	var got res
	mcpServePumpUntil(t, m, m.mcpServe.hub, "answer", func() bool {
		select {
		case got = <-ch:
			return true
		default:
			return false
		}
	})
	if got.err != nil {
		t.Fatalf("read: %v", got.err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(got.s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Spec (MS-1): the listener serves the workspace socket (0600 in a 0700
// dir): initialize, tools/list with the allow-list, tools/call through
// the UI; stop removes the socket, drops the client and leaks no
// goroutine.
func TestMCPServeSocketEndToEnd(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	runtime.GC()
	before := runtime.NumGoroutine()
	if cmd := m.startMCPServe(); cmd == nil || m.mcpServe == nil {
		t.Fatal("not started")
	}
	if m.startMCPServe() != nil {
		t.Fatal("a second start did something")
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket %s: %v %v", path, fi, err)
	}
	if di, err := os.Stat(filepath.Dir(path)); err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode: %v %v", di, err)
	}
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	init := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"fake client"}}}`)
	if init["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "termocode" {
		t.Fatalf("initialize: %v", init)
	}
	list := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := list["result"].(map[string]any)["tools"].([]any)
	want := 0 // the workspace has no database connection: no db_* / redis_* tools (MS-7)
	for _, row := range mcpServeAllowList {
		if (mcpServeDBSet{}).lists(row.name) {
			want++
		}
	}
	if len(tools) != want {
		t.Fatalf("%d tools listed, want %d", len(tools), want)
	}
	call := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"notify","arguments":{"title":"from outside"}}}`)
	if !strings.Contains(mustJSON(t, call), `"text":"shown"`) {
		t.Fatalf("call: %v", call)
	}
	never := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_command","arguments":{"command":"id"}}}`)
	if never["error"].(map[string]any)["code"].(float64) != mcp.CodeInvalidParams {
		t.Fatalf("never tool: %v", never)
	}
	if lines := strings.Join(m.output.Lines(aiAuditChannel), "\n"); !strings.Contains(lines, "by=mcp:fake-client") {
		t.Fatalf("audit: %s", lines)
	}
	m.stopMCPServe()
	if m.mcpServe != nil || mcpServeTarget.Load() != nil {
		t.Fatal("still serving")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left after stop: %v", err)
	}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("client still connected after stop")
	}
	c.Close()
	mcpServeGoroutinesSettle(t, before)
}

func mustJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mcpServeGoroutinesSettle(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	after := runtime.NumGoroutine()
	for after > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		runtime.GC()
		after = runtime.NumGoroutine()
	}
	if after > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines %d → %d\n%s", before, after, buf[:runtime.Stack(buf, true)])
	}
}

// Spec (MS-1): a peer of another uid is closed before any answer (the
// check is swapped in to simulate it; the real one is session.CheckPeer).
func TestMCPServeWrongUIDRefused(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	old := mcpServePeer
	mcpServePeer = func(net.Conn) error { return errors.New("peer uid 4242 is not ours") }
	t.Cleanup(func() { mcpServePeer = old })
	m.startMCPServe()
	defer m.stopMCPServe()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"))
	if line, err := bufio.NewReader(c).ReadString('\n'); err == nil {
		t.Fatalf("a foreign peer got an answer: %s", line)
	}
}

// Spec (MS-1): a stale socket file from a crashed run is replaced; a live
// listener is left alone and the second start is one toast, no crash.
func TestMCPServeStaleAndBusySocket(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = stale.Close()
	m.startMCPServe()
	if m.mcpServe == nil {
		t.Fatalf("stale socket blocked the start: %s", aiToastText(m))
	}
	// A second UI for the same folder: busy, one toast, the first keeps it.
	other, _, _ := mcpServeTestModel(t)
	other.mcpServe = nil
	other.startMCPServe()
	if other.mcpServe != nil || !strings.Contains(aiToastText(other), "MCP server not started") {
		t.Fatalf("busy socket: serving=%v toast=%q", other.mcpServe != nil, aiToastText(other))
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("the busy socket was removed:", err)
	}
	m.stopMCPServe()
}

// Spec (MS-1): quit closes the listener and says no to a waiting card.
func TestMCPServeClosedOnQuit(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	m.startMCPServe()
	src := newMCPServeSource(m.mcpServe.hub, t.TempDir())
	call := mcpServeStartCall(src, "c", "debug_breakpoint", map[string]any{"op": "clear"})
	mcpServePumpUntil(t, m, m.mcpServe.hub, "card", func() bool { return mcpServeCardOpen(m, "c") })
	m.quitNow()
	if m.mcpServe != nil || m.confirmOpen {
		t.Fatal("still serving after quit")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left after quit: %v", err)
	}
	select {
	case got := <-call:
		var ce *mcp.CallError
		if !errors.As(got.err, &ce) || ce.Code != mcp.CodeDeclined {
			t.Fatalf("waiting call: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting call hangs after quit")
	}
	StopMCPServer() // idempotent
}

// Spec (MS-6): over the real socket a stateless client (no initialize)
// discovers the server, lists the resource and reads the editor state;
// the audit line names the client from the request's _meta.
func TestMCPServeSocketStatelessResource(t *testing.T) {
	m, _, _ := mcpServeTestModel(t)
	m.mcpServe = nil
	path := mcpServeShortRuntime(t)
	m.startMCPServe()
	defer m.stopMCPServe()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := bufio.NewReader(c)
	disc := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":1,"method":"server/discover"}`)
	if !strings.Contains(mustJSON(t, disc), `"resources"`) {
		t.Fatalf("discover: %v", disc)
	}
	list := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`)
	if !strings.Contains(mustJSON(t, list), "termocode://editor/state") {
		t.Fatalf("list: %v", list)
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"stateless one"}}`
	read := mcpServeClientLine(t, m, c, r, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"termocode://editor/state",`+meta+`}}`)
	if !strings.Contains(mustJSON(t, read), `branch`) {
		t.Fatalf("read: %v", read)
	}
	if lines := strings.Join(m.output.Lines(aiAuditChannel), "\n"); !strings.Contains(lines, "resources/read") || !strings.Contains(lines, "by=mcp:stateless-one") {
		t.Fatalf("audit: %s", lines)
	}
}
