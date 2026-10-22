//go:build e2e

package e2e

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// relayProc is a running `termocode mcp`, spoken to in JSON lines.
type relayProc struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *strings.Builder
}

func startRelay(t *testing.T, home string, args ...string) *relayProc {
	t.Helper()
	cmd := exec.Command(binPath, append([]string{"mcp"}, args...)...)
	cmd.Env = isolatedEnv(home)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	r := &relayProc{t: t, cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), stderr: &strings.Builder{}}
	cmd.Stderr = r.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return r
}

// call sends one request and returns the decoded reply line.
func (r *relayProc) call(line string) (string, map[string]any) {
	r.t.Helper()
	if _, err := io.WriteString(r.in, line+"\n"); err != nil {
		r.t.Fatal(err)
	}
	ch := make(chan string, 1)
	go func() { s, _ := r.out.ReadString('\n'); ch <- s }()
	select {
	case s := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			r.t.Fatalf("stdout is not JSON: %q (stderr %q)", s, r.stderr.String())
		}
		return s, m
	case <-time.After(15 * time.Second):
		r.t.Fatalf("no reply to %s (stderr %q)", line, r.stderr.String())
		return "", nil
	}
}

// Spec (MS-2): with mcp_server on, `termocode mcp --dir <ws>` relays an
// agent's stdio to the running UI: initialize and tools/list answer from
// the live server, and the process ends when stdin ends.
func TestMCPRelayToRunningUI(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"mcp_server":"on","setup_done":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := NewRepo(t)
	tm := Start(t, StartOpts{Home: home, Workspace: ws})
	tm.WaitText("main.go", 30*time.Second)

	// Wait for the listener: try the relay until it reaches this UI. A
	// socket glob is not enough, another termocode on the machine may
	// have one, so a relay that fell back to static mode tries again.
	var r *relayProc
	var s string
	deadline := time.Now().Add(20 * time.Second)
	for {
		r = startRelay(t, home, "--dir", ws)
		_, m := r.call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"e2e"}}}`)
		if m["result"] == nil {
			t.Fatalf("initialize: %v", m)
		}
		s, _ = r.call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		if !strings.Contains(r.stderr.String(), "not open") && !strings.Contains(r.stderr.String(), "cannot be reached") {
			break
		}
		_ = r.in.Close()
		if time.Now().After(deadline) {
			t.Fatalf("relay never reached the UI: %q", r.stderr.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(s, `"read_file"`) {
		t.Fatalf("tools/list: %.300s", s)
	}
	_ = r.in.Close()
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relay exit: %v (stderr %q)", err, r.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("relay did not exit after stdin closed")
	}
}

// Spec (MS-2): no termocode in the folder: the tool list still answers,
// a tool call fails with a clear -32001 message.
func TestMCPRelayNoUI(t *testing.T) {
	home := t.TempDir()
	ws := NewRepo(t)
	r := startRelay(t, home, "--dir", ws)
	s, _ := r.call(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(s, `"read_file"`) {
		t.Fatalf("static tools/list: %.300s", s)
	}
	s, m := r.call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"main.go"}}}`)
	e, _ := m["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32001) || !strings.Contains(s, "termocode is not open in") {
		t.Fatalf("tools/call: %s", s)
	}
}
