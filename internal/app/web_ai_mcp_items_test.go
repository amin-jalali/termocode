//go:build unix

package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9, AI: MCP Prompts and Resources…): ai.mcp_items lists the
// connected servers' prompts ("/server.prompt ") and resources
// ("@server:uri "), as the laptop's picker does, off the UI thread; the
// page puts a pick in its chat draft (nothing is sent until the user
// sends it).
func TestWebAIMCPItemsListsPromptsAndResources(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	webTestHub(t, m)
	c := webTestSub(t, m)
	call, work := webTestCallLater(t, m, c, webproto.OpAIMCPItems, `{}`)
	if f := webTestAnswer(t, c, call); f != nil || work == nil {
		t.Fatalf("listed on the UI thread: %v", f)
	}
	work()
	f := webTestAnswer(t, c, call)
	if f == nil || f["t"] != "res" {
		t.Fatalf("no list: %v", f)
	}
	var texts []string
	for _, it := range f["data"].(map[string]any)["items"].([]any) {
		x := it.(map[string]any)
		if x["title"] == "" {
			t.Errorf("an item has no title: %v", x)
		}
		texts = append(texts, x["text"].(string))
	}
	all := strings.Join(texts, "|")
	for _, want := range []string{"/fake.review ", "/fake.hello ", "@fake:file:///notes/a.txt ", "@fake:img://chart.png "} {
		if !strings.Contains(all, want) {
			t.Errorf("the list misses %q: %v", want, texts)
		}
	}
}

// Spec (WEBP-9): chat.send with "/server.prompt …" sends the server's
// prompt, as Enter in the laptop's chat does; other text goes as it is.
func TestWebChatSendRunsAnMCPPrompt(t *testing.T) {
	m := covMCPModel(t)
	writeUserMCP(t, mcptest.Config(map[string]map[string]string{"fake": nil}))
	webTestFakeAI(m, "ok")
	webTestHub(t, m)
	c := webTestSub(t, m)
	f, cmd := webTestCall(t, m, c, webproto.OpChatSend, `{"text":"/fake.review main.go style=long"}`)
	if f["t"] != "res" || cmd == nil {
		t.Fatalf("send: %v", f)
	}
	msg, ok := cmd().(aiMCPPromptMsg)
	if !ok || msg.err != nil || !strings.HasPrefix(msg.text, "[MCP prompt fake.review]") {
		t.Fatalf("the prompt was not got: %+v", msg)
	}
	if m.ai.streaming || len(m.ai.conv.Messages) != 0 {
		t.Fatalf("the raw command went to the model: %+v", m.ai.conv.Messages)
	}
}
