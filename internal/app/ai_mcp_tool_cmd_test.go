package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// UX-10: "/tool server.tool key=value …" calls one MCP tool by name from
// the chat. A read-only tool runs at once and the result is a note in the
// chat; any other tool asks first (Cancel first) and runs only on Run.

func TestAIMCPToolParse(t *testing.T) {
	c, ok := aiMCPToolParse(`/tool fake.echo text="hello world" n=3 on=true`)
	if !ok || c.server != "fake" || c.tool != "echo" || c.args["text"] != "hello world" || c.args["n"] != float64(3) || c.args["on"] != true {
		t.Fatalf("parse %+v %v", c, ok)
	}
	c, ok = aiMCPToolParse(`/tool fake.write_note {"title":"a b","body":"x"}`)
	if !ok || c.args["title"] != "a b" || c.args["body"] != "x" {
		t.Fatalf("json args %+v %v", c, ok)
	}
	for _, bad := range []string{"/tool", "/tool fake", "/tool .x", "/toolbox fake.echo", "/fake.review x", "tool fake.echo", "/tool fake.echo oops"} {
		if _, ok := aiMCPToolParse(bad); ok {
			t.Errorf("%q parsed as a tool command", bad)
		}
	}
}

func TestAIMCPToolReadOnlyRunsAtOnce(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	cmd, ok := m.aiMCPToolSubmit(`/tool fake.echo text=hi`)
	if !ok {
		t.Fatal("not taken as a tool command")
	}
	msg := cmd().(aiMCPToolMsg)
	if msg.err != nil || msg.needsYes || !strings.Contains(msg.text, "echo: hi") {
		t.Fatalf("result %+v", msg)
	}
	m.onAIMCPTool(msg)
	var noted bool
	for _, sm := range m.aiS().conv.Messages {
		noted = noted || (sm.Role == ai.StoredNote && strings.Contains(sm.Text, "fake.echo") && strings.Contains(sm.Text, "echo: hi"))
	}
	if !noted {
		t.Fatal("the result is not a note in the chat")
	}
}

func TestAIMCPToolWriteAsksFirst(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	cmd, _ := m.aiMCPToolSubmit(`/tool fake.write_note title=T`)
	msg := cmd().(aiMCPToolMsg)
	if !msg.needsYes || msg.text != "" {
		t.Fatalf("a write ran before the owner said yes: %+v", msg)
	}
	m.onAIMCPTool(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindAIMCPTool {
		t.Fatal("no confirm card")
	}
	if b := m.confirm.Buttons(); b[0].ID != "cancel" {
		t.Fatalf("Cancel is not first: %+v", b)
	}
	if card := cardText(m); !strings.Contains(card, "fake.write_note") || !strings.Contains(card, `"title"`) {
		t.Fatalf("card: %s", card)
	}
	if c := m.runAIMCPToolConfirm("cancel"); c != nil {
		t.Fatal("cancel ran something")
	}
	m.onAIMCPTool(msg)
	run := m.runAIMCPToolConfirm("run")
	if run == nil {
		t.Fatal("Run did nothing")
	}
	done := run().(aiMCPToolMsg)
	if done.err != nil || !strings.Contains(done.text, "wrote T") {
		t.Fatalf("after Run: %+v", done)
	}
}

func TestAIMCPToolUnknownIsNotTaken(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	if _, ok := m.aiMCPToolSubmit(`/tool nope.echo text=hi`); ok {
		t.Error("an unknown server's tool command is taken")
	}
	cmd, _ := m.aiMCPToolSubmit(`/tool fake.nothing`)
	if msg := cmd().(aiMCPToolMsg); msg.err == nil || !strings.Contains(msg.err.Error(), "no tool") {
		t.Fatalf("unknown tool: %+v", msg)
	}
}
