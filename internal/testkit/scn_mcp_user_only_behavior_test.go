package testkit

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

// SCN-2 for outside agents: an MCP client on the UI's MCP socket (what
// `termocode mcp` relays to) can not do what only the user may do.

// scnMCP is one MCP client connection to the running app.
type scnMCP struct {
	t     *testing.T
	d     *Driver
	conn  net.Conn
	lines chan string
	id    int
}

func dialSCNMCP(t *testing.T, d *Driver) *scnMCP {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	p, err := session.PathsFor(wd)
	if err != nil {
		t.Fatal(err)
	}
	var conn net.Conn
	if !d.WaitFor(func() bool {
		conn, err = net.Dial("unix", p.MCPSocket)
		return err == nil
	}, 10*time.Second) {
		t.Fatalf("no MCP socket at %s: %v", p.MCPSocket, err)
	}
	c := &scnMCP{t: t, d: d, conn: conn, lines: make(chan string, 16)}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		for sc.Scan() {
			c.lines <- sc.Text()
		}
		close(c.lines)
	}()
	c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "scn-agent"}}, nil)
	c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return c
}

func (c *scnMCP) send(msg map[string]any) {
	c.t.Helper()
	b, _ := json.Marshal(msg)
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

// call sends one request and pumps the app until its reply comes. Each
// card the call opens is answered with the next of keys (none: a card
// fails the test). It returns the raw reply and the number of cards.
func (c *scnMCP) call(method string, params map[string]any, keys []string) (string, int) {
	c.t.Helper()
	c.id++
	c.send(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	cards := 0
	deadline := time.Now().Add(30 * time.Second)
	for {
		var reply string
		got := false
		c.d.WaitFor(func() bool {
			select {
			case reply, got = <-c.lines:
				return true
			default:
			}
			return strings.Contains(screen(c.d), "Asked by an outside agent")
		}, 5*time.Second)
		if got {
			var m struct {
				ID int `json:"id"`
			}
			if json.Unmarshal([]byte(reply), &m) == nil && m.ID == c.id {
				return reply, cards
			}
			continue // a notification
		}
		if strings.Contains(screen(c.d), "Asked by an outside agent") {
			if len(keys) == 0 {
				c.t.Fatalf("%s %v opened a card:\n%s", method, params, screen(c.d))
			}
			before := screen(c.d)
			c.d.Key(keys[cards%len(keys)])
			cards++
			c.d.WaitFor(func() bool { return screen(c.d) != before }, 5*time.Second)
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("%s %v: no reply:\n%s", method, params, screen(c.d))
		}
	}
}

// tool calls a tool and reports whether it was refused (a JSON-RPC error
// or an error result).
func (c *scnMCP) tool(name string, args map[string]any, keys ...string) (reply string, refused bool, cards int) {
	c.t.Helper()
	reply, cards = c.call("tools/call", map[string]any{"name": name, "arguments": args}, keys)
	var m struct {
		Error  *struct{ Code int } `json:"error"`
		Result *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(reply), &m); err != nil {
		c.t.Fatalf("%s: bad reply %q", name, reply)
	}
	return reply, m.Error != nil || (m.Result != nil && m.Result.IsError), cards
}

// scnMCPUserOnlyTools are tools an outside agent must never get: file
// writes, shells, git writes, settings, layout, agent runs, outside agents,
// AI profiles and keys, service access.
var scnMCPUserOnlyTools = []toolCall{
	{"write_file", map[string]any{"path": "a.txt", "content": "x"}},
	{"create_file", map[string]any{"path": "new.txt"}},
	{"create_dir", map[string]any{"path": "newdir"}},
	{"delete", map[string]any{"path": "a.txt"}},
	{"rename", map[string]any{"from": "a.txt", "to": "z.txt"}},
	{"run_command", map[string]any{"command": "touch ran.txt"}},
	{"terminal_send", map[string]any{"text": "touch typed.txt"}},
	{"git_stage", map[string]any{"path": "a.txt"}},
	{"git_commit", map[string]any{"message": "x"}},
	{"git_checkout", map[string]any{"branch": "x", "create": true}},
	{"git_push", map[string]any{}},
	{"git_pull", map[string]any{}},
	{"settings_set", map[string]any{"key": "theme", "value": "light"}},
	{"settings_list", map[string]any{}},
	{"layout_set", map[string]any{"zen": true}},
	{"agent_run_start", map[string]any{"task": "x", "steps": []any{map[string]any{"title": "s", "prompt": "p"}}}},
	{"outside_agent_start", map[string]any{"agent": "claude"}},
	{"profile_switch", map[string]any{"name": "fast"}},
	{"model_slot_draft", map[string]any{"name": "s", "provider": "openai", "model": "m"}},
	{"service_set_access", map[string]any{"name": "github", "writes": true}},
	{"http_set_environment", map[string]any{"name": "dev"}},
	{"http_save_request", map[string]any{"path": "a.http", "name": "x", "method": "GET", "url": "http://localhost/"}},
	{"profiles_list", map[string]any{}},
	{"models_list", map[string]any{}},
	{"mcp_services_list", map[string]any{}},
}

// Spec (SCN-2): over MCP, tools/list offers none of the user-only tools
// and calling one fails; every palette id marked never is refused with no
// card; every confirm id is refused or asks on a card where Esc and Enter
// both refuse; secrets and paths outside the workspace (also through a
// symlink) are refused; nothing changes on disk, in git or in settings.
func TestSCNMCPUserOnlyActionsRefused(t *testing.T) {
	r := newSCNRig(t, `"mcp_server":"on"`)
	c := dialSCNMCP(t, r.d)
	before := r.snapshot(t)

	list, _ := c.call("tools/list", map[string]any{}, nil)
	for _, tc := range scnMCPUserOnlyTools {
		if strings.Contains(list, `"name":"`+tc.name+`"`) {
			t.Errorf("tools/list offers %s", tc.name)
		}
		if _, refused, _ := c.tool(tc.name, tc.args); !refused {
			t.Errorf("%s ran for an outside agent", tc.name)
		}
	}
	if strings.Contains(list, `"name":"mcp__`) {
		t.Error("tools/list re-exports a tool of the user's MCP servers")
	}

	for _, p := range scnNeverActions(t) {
		if reply, refused, _ := c.tool("execute_action", map[string]any{"id": p.ID}); !refused {
			t.Errorf("execute_action %s (never) ran: %s", p.ID, reply)
		}
	}
	asked := 0
	for i, p := range scnConfirmActions(t) {
		key := []string{[]string{"esc", "enter"}[i%2]}
		reply, refused, cards := c.tool("execute_action", map[string]any{"id": p.ID}, key...)
		asked += cards
		if !refused {
			t.Errorf("execute_action %s (confirm) ran after a deny: %s", p.ID, reply)
		}
	}
	t.Logf("%d confirm ids asked on a card, the rest were refused at once", asked)

	for _, e := range []toolCall{
		{"read_file", map[string]any{"path": "linked/secret.txt"}},
		{"read_file", map[string]any{"path": "../outside/secret.txt"}},
		{"read_file", map[string]any{"path": "http-client.private.env.json"}},
		{"list_dir", map[string]any{"path": "linked"}},
	} {
		if reply, refused, _ := c.tool(e.name, e.args); !refused || strings.Contains(reply, r.secret) {
			t.Errorf("%s %v: refused=%v reply %s", e.name, e.args, refused, reply)
		}
	}
	if reply, _, _ := c.tool("search", map[string]any{"query": r.secret}); strings.Contains(reply, "private.env") {
		t.Errorf("search found the secret file: %s", reply)
	}

	if r.d.Quit() {
		t.Error("the app quit")
	}
	if b, err := os.ReadFile(r.ran); err == nil {
		t.Errorf("outside agents started: %s", b)
	}
	if diff := before.diff(r.snapshot(t)); len(diff) > 0 {
		t.Errorf("an outside agent changed things:\n%s", strings.Join(diff, "\n"))
	}
}
