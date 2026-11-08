package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// A Codex (or Claude Code) step gets termocode's own tools, the full
// set of the step (databases, containers and servers, files, terminal,
// git, …), through an MCP server of its own: a private socket the CLI
// reaches with `termocode mcp --socket`. Every call goes through the
// same gate as a model step's: a read runs at once, a change asks on
// the chat's card first, results are redacted, the step card shows each
// call. The step's Tools setting decides which tools are served.

// stepMCPClient is a tiny MCP client over the step's socket. It runs on
// the step's goroutine, so it reports with t.Error, never t.Fatal.
type stepMCPClient struct {
	c  net.Conn
	br *bufio.Reader
	id int
}

func dialStepMCP(t *testing.T, s clistep.Spec) *stepMCPClient {
	t.Helper()
	if s.MCP == nil {
		t.Error("the step got no MCP server")
		return nil
	}
	sock := ""
	for i, a := range s.MCP.Args {
		if a == "--socket" && i+1 < len(s.MCP.Args) {
			sock = s.MCP.Args[i+1]
		}
	}
	if s.MCP.Name != "termocode" || len(s.MCP.Args) == 0 || s.MCP.Args[0] != "mcp" || sock == "" {
		t.Errorf("MCP server %+v", s.MCP)
		return nil
	}
	if fi, err := os.Stat(filepath.Dir(sock)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("socket folder %s is not private: %v %v", filepath.Dir(sock), fi.Mode(), err)
	}
	c, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		t.Errorf("dial %s: %v", sock, err)
		return nil
	}
	cl := &stepMCPClient{c: c, br: bufio.NewReader(c)}
	cl.call(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "codex", "version": "1"}})
	return cl
}

func (cl *stepMCPClient) call(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	cl.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": cl.id, "method": method, "params": params})
	if _, err := cl.c.Write(append(b, '\n')); err != nil {
		t.Error(err)
		return nil
	}
	_ = cl.c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		line, err := cl.br.ReadBytes('\n')
		if err != nil {
			t.Errorf("%s: %v", method, err)
			return nil
		}
		var r struct {
			ID     *int           `json:"id"`
			Result map[string]any `json:"result"`
			Error  map[string]any `json:"error"`
		}
		if json.Unmarshal(line, &r) != nil || r.ID == nil || *r.ID != cl.id {
			continue // a notification
		}
		if r.Error != nil {
			return map[string]any{"error": r.Error}
		}
		return r.Result
	}
}

func (cl *stepMCPClient) tools(t *testing.T) []string {
	t.Helper()
	var names []string
	list, _ := cl.call(t, "tools/list", map[string]any{})["tools"].([]any)
	for _, x := range list {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	return names
}

func (cl *stepMCPClient) text(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	r := cl.call(t, "tools/call", map[string]any{"name": name, "arguments": args})
	if e, ok := r["error"]; ok {
		b, _ := json.Marshal(e)
		return "rpc error: " + string(b)
	}
	var b strings.Builder
	content, _ := r["content"].([]any)
	for _, c := range content {
		b.WriteString(c.(map[string]any)["text"].(string))
	}
	return b.String()
}

// stepMCPRun runs the GPT profile (tools preset) with a fake Codex that
// works through the step's MCP server with fn.
func stepMCPRun(t *testing.T, preset string, fn func(cl *stepMCPClient), onConfirm func(aiChatConfirmMsg) string) (*Model, string, clistep.Spec) {
	t.Helper()
	m, dir := gptRunModel(t)
	p := gptProfile()
	p.Steps[0].Tools.Preset = preset
	m.ai.profFake = []ai.Profile{p}
	m.ai.reloadProfiles()
	m.output = newOutputStore()
	// the file the step reads carries a secret the IDE knows: the result hides it
	ai.RegisterSecret("cli-mcp-registered-secret-31")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("token cli-mcp-registered-secret-31\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got clistep.Spec
	old := aiCLIRun
	t.Cleanup(func() { aiCLIRun = old })
	aiCLIRun = func(_ context.Context, s clistep.Spec, _ string, emit func(ai.Event)) (clistep.Result, error) {
		mu.Lock()
		got = s
		mu.Unlock()
		if fn != nil {
			if cl := dialStepMCP(t, s); cl != nil {
				defer cl.c.Close()
				fn(cl)
			}
		}
		emit(ai.Event{Kind: ai.EventText, Text: "done"})
		return clistep.Result{Text: "done"}, nil
	}
	driveAIChat(t, m, m.sendAIChat("how many tables does baaytona-db-1 have?"), onConfirm)
	mu.Lock()
	defer mu.Unlock()
	return m, dir, got
}

func hasName(names []string, n string) bool {
	for _, x := range names {
		if x == n {
			return true
		}
	}
	return false
}

func TestAICLIStepGetsTermocodeTools(t *testing.T) {
	asked := 0
	var names []string
	var read, wrote string
	m, _, spec := stepMCPRun(t, agentdag.PresetAll, func(cl *stepMCPClient) {
		names = cl.tools(t)
		read = cl.text(t, "read_file", map[string]any{"path": "a.txt"})
		wrote = cl.text(t, "run_command", map[string]any{"command": "touch b.txt"})
	}, func(c aiChatConfirmMsg) string {
		asked++
		if c.tool.Name != "run_command" || c.step != "GPT" {
			t.Errorf("card for %s from step %q", c.tool.Name, c.step)
		}
		return "deny"
	})
	for _, want := range []string{"read_file", "write_file", "run_command", "db_connections", "db_query", "contexts_list", "containers_list", "git_status", "terminal_send"} {
		if !hasName(names, want) {
			t.Errorf("tools/list misses %s: %v", want, names)
		}
	}
	for _, n := range names {
		if strings.HasPrefix(n, "mcp__") {
			t.Errorf("a user's MCP tool is served again: %s", n)
		}
	}
	if !strings.Contains(read, "token") || strings.Contains(read, "cli-mcp-registered-secret-31") {
		t.Errorf("read_file result not redacted: %q", read)
	}
	if asked != 1 || !strings.Contains(wrote, "declined") {
		t.Errorf("run_command: %d cards, result %q", asked, wrote)
	}
	if _, err := os.Stat("b.txt"); err == nil {
		t.Error("a declined command ran")
	}
	st := aiFindStep(aiRunMsg(t, m), "s1")
	if st == nil || len(st.Tools) < 2 {
		t.Fatalf("the step card shows no termocode calls: %+v", st)
	}
	if !spec.Write {
		t.Error("Tools: all must give the CLI its write sandbox")
	}
	if spec.MCP != nil {
		for i, a := range spec.MCP.Args {
			if a == "--socket" {
				if _, err := os.Stat(spec.MCP.Args[i+1]); err == nil {
					t.Error("the step's socket is still there after the step")
				}
			}
		}
	}
}

// Read-only serves no tool that changes anything; none serves nothing.
func TestAICLIStepToolsFollowPreset(t *testing.T) {
	var names []string
	_, _, spec := stepMCPRun(t, agentdag.PresetReadOnly, func(cl *stepMCPClient) { names = cl.tools(t) }, nil)
	if spec.Write || !hasName(names, "read_file") || !hasName(names, "db_schema") || hasName(names, "write_file") || hasName(names, "run_command") {
		t.Errorf("read-only: write %v tools %v", spec.Write, names)
	}
	_, _, spec = stepMCPRun(t, agentdag.PresetNone, nil, nil)
	if spec.MCP != nil || spec.Write {
		t.Errorf("none: %+v", spec)
	}
}
