package app

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/acp"
)

// acpFakeServe plays an ACP agent on the pipe: it answers initialize and
// session/new, and on a prompt asks to write note.txt, then replies.
func acpFakeServe(t *testing.T, r io.Reader, w io.Writer, root string) {
	in := bufio.NewReader(r)
	send := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = w.Write(append(b, '\n'))
	}
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch m["method"] {
		case "initialize":
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"sessionId": "s1"}})
		case "session/prompt":
			pid := m["id"]
			send(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "fs/write_text_file",
				"params": map[string]any{"sessionId": "s1", "path": filepath.Join(root, "note.txt"), "content": "new line\n"}})
			for { // wait for the client's answer to the write
				line, err := in.ReadBytes('\n')
				if err != nil {
					return
				}
				var a map[string]any
				if json.Unmarshal(line, &a) == nil && a["id"] == float64(900) {
					break
				}
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1",
				"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "hello from agent"}}}})
			send(map[string]any{"jsonrpc": "2.0", "id": pid, "result": map[string]any{"stopReason": "end_turn"}})
		}
	}
}

func acpTestSetup(t *testing.T, root string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "acp_agents.json")
	if err := os.WriteFile(cfg, []byte(`{"agents":[{"name":"Helper","command":"helper-agent"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath, oldStart := acpAgentsPath, acpStartAgent
	acpAgentsPath = func() (string, error) { return cfg, nil }
	acpStartAgent = func(a acp.Agent, dir string, h acp.Handler) (*acp.Client, func(), error) {
		cr, aw := io.Pipe()
		ar, cw := io.Pipe()
		go acpFakeServe(t, ar, aw, root)
		c := acp.NewClient(cr, cw, h)
		return c, func() { c.Close(); aw.Close(); ar.Close() }, nil
	}
	t.Cleanup(func() { acpAgentsPath, acpStartAgent = oldPath, oldStart })
}

// Spec (CMP-83): Agent: Chat with an ACP Agent… lists the agents, starts
// the chosen one as the chat's thread; a message goes to the agent (not
// the provider), its file write waits on a Cancel-first card with the
// diff, and its reply lands in the chat.
func TestACPThreadChatAndWriteCard(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	acpTestSetup(t, root)

	m.toggleACPThread()
	if !m.pickerOpen || m.pickerKind != pickerKindACPAgents {
		t.Fatal("no agent picker")
	}
	m.pickerOpen = false
	aiShipDrive(t, m, m.onACPAgentPicked("0"), func() bool { return m.ai.acp != nil })

	aiShipDrive(t, m, m.sendAIChat("write a note"), func() bool { return m.confirmOpen && m.confirmKind == confirmKindACPWrite })
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("write card is not Cancel-first: %q", m.confirm.SafeID())
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); err == nil {
		t.Fatal("written before the card")
	}
	aiShipDrive(t, m, m.answerAIRulesForTest("write"), func() bool {
		ms := m.ai.conv.Messages
		return !m.ai.streaming && len(ms) > 0 && strings.Contains(ms[len(ms)-1].Text, "hello from agent")
	})
	if b, _ := os.ReadFile(filepath.Join(root, "note.txt")); string(b) != "new line\n" {
		t.Errorf("note.txt %q", b)
	}
	if len(rec.reqs) != 0 {
		t.Errorf("the provider got %d requests: the message must go to the agent", len(rec.reqs))
	}
	m.toggleACPThread() // again: ends the thread
	if m.ai.acp != nil {
		t.Error("the thread did not end")
	}
}

// Spec (CMP-83): a permission request is a card whose safe button is the
// agent's reject option (Cancel when it has none); Esc answers cancelled.
func TestACPPermissionCard(t *testing.T) {
	m, _, root, _ := aiSkillsModel(t)
	th := &acpThread{name: "Helper", root: root, asks: make(chan *acpAsk, 4)}
	m.ai.acp = th
	ask := &acpAsk{perm: &acp.PermissionRequest{Tool: acp.ToolCall{ID: "t1", Title: "Run go test", Kind: "execute"},
		Options: []acp.PermissionOption{{ID: "ok", Name: "Allow", Kind: "allow_once"}, {ID: "no", Name: "Reject", Kind: "reject_once"}}},
		reply: make(chan string, 1)}
	m.onACPAsk(acpAskMsg{t: th, ask: ask})
	if !m.confirmOpen || m.confirmKind != confirmKindACPPermission {
		t.Fatal("no permission card")
	}
	if m.confirm.SafeID() != "opt:no" {
		t.Fatalf("safe %q", m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if got := <-ask.reply; got != "" {
		t.Fatalf("Esc chose %q", got)
	}

	ask2 := &acpAsk{perm: &acp.PermissionRequest{Tool: acp.ToolCall{ID: "t2", Title: "Edit"},
		Options: []acp.PermissionOption{{ID: "go", Name: "Allow", Kind: "allow_once"}}}, reply: make(chan string, 1)}
	m.onACPAsk(acpAskMsg{t: th, ask: ask2})
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("no reject option: safe %q", m.confirm.SafeID())
	}
	m.answerAIRulesForTest("opt:go")
	if got := <-ask2.reply; got != "go" {
		t.Fatalf("chose %q", got)
	}
}

func TestACPWriteCardText(t *testing.T) {
	root := t.TempDir()
	text := acpWriteCardText("Helper", root, filepath.Join(root, "a", "b.go"), "x\n", "x\ny\n")
	for _, want := range []string{"Helper", filepath.Join("a", "b.go"), "+y"} {
		if !strings.Contains(text, want) {
			t.Errorf("card lacks %q:\n%s", want, text)
		}
	}
}

func TestACPPolicy(t *testing.T) {
	for _, id := range []string{"acp-thread", "acp-agents-edit"} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s: policy %+v ok=%v", id, p, ok)
		}
	}
}
