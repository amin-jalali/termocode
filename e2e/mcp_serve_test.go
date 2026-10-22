//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sendAsync writes one request and returns a channel for the next reply
// line, for a call that waits for a card.
func (r *relayProc) sendAsync(line string) <-chan string {
	r.t.Helper()
	if _, err := io.WriteString(r.in, line+"\n"); err != nil {
		r.t.Fatal(err)
	}
	ch := make(chan string, 1)
	go func() { s, _ := r.out.ReadString('\n'); ch <- s }()
	return ch
}

// reply waits for the line from sendAsync.
func (r *relayProc) reply(ch <-chan string, what string) (string, map[string]any) {
	r.t.Helper()
	select {
	case s := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			r.t.Fatalf("%s: stdout is not JSON: %q (stderr %q)", what, s, r.stderr.String())
		}
		return s, m
	case <-time.After(20 * time.Second):
		r.t.Fatalf("%s: no reply (stderr %q)", what, r.stderr.String())
		return "", nil
	}
}

func waitFile(t *testing.T, p string, want bool, what string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for exists(p) != want && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if exists(p) != want {
		t.Fatalf("%s (file %s, want exists=%v)", what, filepath.Base(p), want)
	}
}

// Spec (MS-5): an outside agent over `termocode mcp --dir`. A read tool
// answers at once. tasks_run shows a card with the agent's name; Deny gives
// -32000 and the task does not run. Always allow runs the call, and the
// next call runs without a card. The status screen lists the agent.
func TestMCPServeCardDenyAndAlwaysAllow(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "termocode")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfg, "config.json")
	if err := os.WriteFile(cfgFile, []byte(`{"mcp_server":"on","setup_done":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ws := NewRepo(t)
	marker := filepath.Join(ws, "task-ran.marker")
	if err := os.MkdirAll(filepath.Join(ws, ".termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	tasks := `{"version":1,"tasks":[{"label":"mark","command":"touch task-ran.marker"}]}`
	if err := os.WriteFile(filepath.Join(ws, ".termocode", "tasks.json"), []byte(tasks), 0o644); err != nil {
		t.Fatal(err)
	}

	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: home, Workspace: ws})
	tm.WaitText("main.go", bootTimeout)

	// Connect once the listener is up: a relay that finds no socket runs in
	// static mode ("not open" on stderr), so try again until it relays.
	var r *relayProc
	var m map[string]any
	deadline := time.Now().Add(20 * time.Second)
	for {
		r = startRelay(t, home, "--dir", ws)
		_, m = r.call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"e2e-agent"}}}`)
		if !strings.Contains(r.stderr.String(), "not open") {
			break
		}
		_ = r.in.Close()
		_ = r.cmd.Process.Kill()
		if time.Now().After(deadline) {
			t.Fatal("the relay never reached the UI")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if m["result"] == nil {
		t.Fatalf("initialize: %v", m)
	}
	if _, err := io.WriteString(r.in, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n"); err != nil {
		t.Fatal(err)
	}

	// A read tool answers without a card.
	s, m := r.call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"tasks_list","arguments":{}}}`)
	if m["error"] != nil || !strings.Contains(s, "mark") {
		t.Fatalf("tasks_list: %s", s)
	}
	if tm.Has("wants to run") {
		t.Fatal("a read tool opened a card")
	}

	call := `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"tasks_run","arguments":{"label":"mark"}}}`
	req := func(id string) string { return strings.Replace(call, "%d", id, 1) }

	// Deny: the card names the agent, Esc refuses, the task does not run.
	ch := r.sendAsync(req("3"))
	tm.WaitText("e2e-agent wants to run tasks_run", uiTimeout)
	tm.Send(KeyEsc)
	s, m = r.reply(ch, "denied call")
	e, _ := m["error"].(map[string]any)
	if e == nil || e["code"] != float64(-32000) {
		t.Fatalf("denied call: want error -32000, got %s", s)
	}
	tm.WaitGone("wants to run", uiTimeout)
	time.Sleep(time.Second)
	if exists(marker) {
		t.Fatal("the task ran although the user denied it")
	}

	// Always allow: Allow, Always allow, Deny are the buttons; move once right.
	ch = r.sendAsync(req("4"))
	tm.WaitText("e2e-agent wants to run tasks_run", uiTimeout)
	tm.WaitText("Always allow", uiTimeout)
	tm.Send("\x1b[C")
	tm.settle()
	tm.Send(KeyEnter)
	s, m = r.reply(ch, "always-allowed call")
	if m["error"] != nil {
		t.Fatalf("always-allowed call: %s", s)
	}
	waitFile(t, marker, true, "the task did not run after Always allow")
	b, _ := os.ReadFile(cfgFile)
	if !strings.Contains(string(b), "mcp_server_auto_approve") || !strings.Contains(string(b), "e2e-agent") || !strings.Contains(string(b), "tasks_run") {
		t.Fatalf("config.json has no remembered allow: %s", b)
	}

	// The second call runs at once, with no card.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	ch = r.sendAsync(req("5"))
	s, m = r.reply(ch, "second call")
	if m["error"] != nil {
		t.Fatalf("second call: %s", s)
	}
	// The call finished although nobody pressed a key: no card was needed.
	// The screen may still show the old card for a moment; it must go.
	tm.WaitGone("wants to run", uiTimeout)
	waitFile(t, marker, true, "the second call did not run the task")

	// The status screen lists the agent.
	tm.Palette("MCP: Server Status")
	tm.WaitText("MCP server status", uiTimeout)
	tm.WaitText("Listening", uiTimeout)
	tm.WaitText("e2e-agent", uiTimeout)
	tm.Send(KeyEsc)
	tm.settle()
	tm.Quit()
}
