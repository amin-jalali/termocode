//go:build unix

package testkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// SCN-26: MCP under stress. Two outside agents use the app's MCP socket
// at the same time; a server the chat agent uses crashes in the middle
// of a call and comes back; a secret the app knows shows up in a tool's
// output and is hidden on both sides (from the model and from an outside
// agent).

// sendCall sends one request without waiting and returns its id.
func (c *scnMCP) sendCall(method string, params map[string]any) int {
	c.t.Helper()
	c.id++
	c.send(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	return c.id
}

// poll returns the reply to id if one came (notifications and other
// replies are dropped), without blocking.
func (c *scnMCP) poll(id int) string {
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				return ""
			}
			var m struct {
				ID int `json:"id"`
			}
			if json.Unmarshal([]byte(line), &m) == nil && m.ID == id {
				return line
			}
		default:
			return ""
		}
	}
}

// Spec (SCN-26): two outside agents on the MCP socket send their calls at
// the same time and each gets its own answer; a file that holds a secret
// the app knows is read with the secret hidden; when one agent goes away
// the other keeps working.
func TestSCNMCPTwoClientsBehavior(t *testing.T) {
	r := newSCNRig(t, `"mcp_server":"on"`)
	ai.RegisterSecret(r.secret) // as if the user had typed it as a key
	if err := os.WriteFile(filepath.Join(r.d.Dir, "leak.txt"), []byte("token is "+r.secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	one, two := dialSCNMCP(t, r.d), dialSCNMCP(t, r.d)

	id1 := one.sendCall("tools/call", map[string]any{"name": "read_file", "arguments": map[string]any{"path": "leak.txt"}})
	id2 := two.sendCall("tools/list", map[string]any{})
	var got1, got2 string
	if !r.d.WaitFor(func() bool {
		if got1 == "" {
			got1 = one.poll(id1)
		}
		if got2 == "" {
			got2 = two.poll(id2)
		}
		return got1 != "" && got2 != ""
	}, 30*time.Second) {
		t.Fatalf("not both agents got an answer: one %q two %q\n%s", got1, got2, screen(r.d))
	}
	if strings.Contains(got1, r.secret) || !strings.Contains(got1, "[REDACTED]") {
		t.Errorf("read_file showed the secret to an outside agent: %s", got1)
	}
	if !strings.Contains(got2, `"name":"read_file"`) {
		t.Errorf("the second agent got the wrong answer: %.300s", got2)
	}

	_ = one.conn.Close()
	reply, refused, _ := two.tool("read_file", map[string]any{"path": "leak.txt"})
	if refused || strings.Contains(reply, r.secret) || !strings.Contains(reply, "[REDACTED]") {
		t.Errorf("after the first agent left: refused=%v reply %s", refused, reply)
	}
	if strings.Contains(screen(r.d), r.secret) {
		t.Errorf("the secret is on the screen:\n%s", screen(r.d))
	}
	if r.d.Quit() {
		t.Error("the app quit")
	}
}

// Spec (SCN-26): the chat agent's MCP server crashes in the middle of a
// call. The turn does not hang and the app keeps running; the next call
// starts the server again and works. The server repeats a secret it was
// given in its output: the model gets "[REDACTED]", and the secret is
// nowhere on the screen.
func TestSCNMCPServerCrashRestartBehavior(t *testing.T) {
	f := newMCPFake(t)
	secret := scnCanary()
	ai.RegisterSecret(secret) // the value of a ${secret:…} the server got
	crashed := filepath.Join(t.TempDir(), "crashed")
	cfg := mcptest.Config(map[string]map[string]string{"fake": {
		"FAKE_LOG": f.log, "FAKE_MARKER": f.marker, "FAKE_CRASH_ONCE": crashed, "FAKE_LEAK": secret,
	}})
	s, srv := fakeToolModel(t,
		toolCall{"mcp__fake__echo", map[string]any{"text": "a"}},
		toolCall{"mcp__fake__echo", map[string]any{"text": "b"}},
	)
	d := mcpDriver(t, srv.URL, cfg, "", "")
	askAI(d, "echo twice")
	waitDone(t, d)

	if _, err := os.Stat(crashed); err != nil {
		t.Fatalf("the fake never crashed (log %q)", f.received())
	}
	res := s.toolResults()
	if len(res) != 2 {
		t.Fatalf("tool results %q", res)
	}
	if !strings.HasPrefix(res[1], "echo: b") {
		t.Errorf("the call after the crash did not run on a new server: %q", res[1])
	}
	for i, x := range res {
		if strings.Contains(x, secret) {
			t.Errorf("tool result %d shows the secret to the model: %q", i, x)
		}
	}
	if !strings.Contains(res[1], "[REDACTED]") {
		t.Errorf("the repeated secret was not hidden: %q", res[1])
	}
	starts := 0
	for _, l := range strings.Split(f.received(), "\n") {
		if l == "initialize" {
			starts++
		}
	}
	if starts < 2 {
		t.Errorf("the server was started %d times, want 2 (log %q)", starts, f.received())
	}
	if strings.Contains(screen(d), secret) {
		t.Errorf("the secret is on the screen:\n%s", screen(d))
	}
	if d.Quit() {
		t.Error("the app quit")
	}
}
