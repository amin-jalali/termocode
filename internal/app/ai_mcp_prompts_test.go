package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

func TestAIMCPPromptParseAndBind(t *testing.T) {
	c, ok := aiMCPPromptParse(`/fake.review "a b.go" style=long`)
	if !ok || c.server != "fake" || c.prompt != "review" || strings.Join(c.args, "|") != "a b.go|style=long" {
		t.Fatalf("parse %+v %v", c, ok)
	}
	for _, bad := range []string{"hello", "/review", "/.x", "/a/b.c"} {
		if _, ok := aiMCPPromptParse(bad); ok {
			t.Errorf("%q parsed as a prompt command", bad)
		}
	}
	p := mcp.Prompt{Name: "review", Arguments: []mcp.PromptArgument{{Name: "file", Required: true}, {Name: "style"}}}
	got, err := aiMCPBindArgs(p, []string{"style=long", "x.go"})
	if err != nil || got["file"] != "x.go" || got["style"] != "long" {
		t.Fatalf("bind %v %v", got, err)
	}
	if _, err := aiMCPBindArgs(p, []string{"style=long"}); err == nil || !strings.Contains(err.Error(), "needs file (file [style])") {
		t.Fatalf("missing: %v", err)
	}
	if _, err := aiMCPBindArgs(p, []string{"a", "b", "c"}); err == nil {
		t.Fatal("too many values accepted")
	}
}

// "/fake.review main.go style=long" gets the server's prompt; its text
// is the message sent (assistant parts marked).
func TestAIMCPPromptSubmitWithFake(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	cmd, ok := m.aiMCPPromptSubmit("/fake.review main.go style=long")
	if !ok {
		t.Fatal("not a prompt command")
	}
	msg := cmd().(aiMCPPromptMsg)
	want := "[MCP prompt fake.review]\nReview main.go (style: long).\n\n(Assistant, from the prompt:) I will look at it."
	if msg.err != nil || msg.text != want {
		t.Fatalf("prompt %q %v", msg.text, msg.err)
	}
	msg = func() aiMCPPromptMsg { c, _ := m.aiMCPPromptSubmit("/fake.review"); return c().(aiMCPPromptMsg) }()
	if msg.err == nil || !strings.Contains(msg.err.Error(), "needs file") {
		t.Fatalf("missing argument: %v", msg.err)
	}
	m.aiSetInput("/fake.review")
	if m.onAIMCPPrompt(msg) == nil || string(m.aiS().input) != "/fake.review" || !strings.Contains(covToastText(m), "MCP prompt not sent") {
		t.Errorf("error kept no draft / toast: %q", covToastText(m))
	}
	if _, ok := m.aiMCPPromptSubmit("/nope.review x"); ok {
		t.Error("an unknown server's command is taken")
	}
}

// Tab loads the prompts, completes the command, then argument values
// from the server (completion/complete).
func TestAIMCPPromptTab(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	m.aiSetInput("/fake.r")
	loaded, ok := covCollect[aiMCPPromptsMsg](m.aiMCPPromptTab())
	if !ok || len(loaded.prompts["fake"]) != 2 {
		t.Fatalf("load %+v", loaded)
	}
	m.onAIMCPPrompts(loaded)
	if got := string(m.aiS().input); got != "/fake.review " {
		t.Fatalf("command completion %q", got)
	}
	m.aiSetInput("/fake.review ma")
	done, ok := covCollect[aiMCPCompleteMsg](m.aiMCPPromptTab())
	if !ok || done.arg != "file" {
		t.Fatalf("complete %+v", done)
	}
	m.onAIMCPComplete(done)
	if got := string(m.aiS().input); got != "/fake.review main" || !strings.Contains(covToastText(m), "main_test.go") {
		t.Fatalf("value completion %q / %q", got, covToastText(m))
	}
	m.aiSetInput("/fake.review main.go style=l")
	done, _ = covCollect[aiMCPCompleteMsg](m.aiMCPPromptTab())
	m.onAIMCPComplete(done)
	if got := string(m.aiS().input); got != "/fake.review main.go style=long " {
		t.Fatalf("named completion %q", got)
	}
	m.aiSetInput("/fake.")
	m.aiMCPPromptTab()
	if got := string(m.aiS().input); got != "/fake." || !strings.Contains(covToastText(m), "/fake.hello") {
		t.Fatalf("several commands %q / %q", got, covToastText(m))
	}
}
