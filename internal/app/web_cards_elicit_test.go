package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestElicit opens an MCP server's question as the chat would.
func webTestElicit(m *Model, req mcp.ElicitRequest) chan mcp.ElicitResult {
	reply := make(chan mcp.ElicitResult, 1)
	m.aiS().mcpx.elicit = &aiMCPElicitPending{req: req, reply: reply, values: map[string]any{}}
	m.mcpElicitCard()
	return reply
}

// MOB-12: an MCP server's plain question is a card with a form; Send
// with valid answers accepts them on the laptop (converted), closes the
// laptop's dialog, and the audit names fields, never values.
func TestWebElicitFormCardSend(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	reply := webTestElicit(&m, mcp.ElicitRequest{Server: "gh", Mode: mcp.ElicitForm, Message: "Which repo?",
		Fields: []mcp.ElicitField{{Name: "repo", Required: true}, {Name: "count", Type: "integer"}}})
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 1 || len(cards[0].Form) != 2 || cards[0].Kind != webproto.CardMCP {
		t.Fatalf("cards = %+v", cards)
	}
	if ch := cards[0].Choices; len(ch) != 2 || ch[0].ID != webproto.ChoiceDeny || !ch[0].Safe || ch[1].ID != webproto.ChoiceSend || ch[1].NeedsLaptop {
		t.Fatalf("choices = %+v", ch)
	}
	// A bad answer is refused and the question stays open.
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"send","form":{"count":"3"}}`); f["t"] != "err" {
		t.Fatalf("missing required: %v", f)
	}
	if m.aiS().mcpx.elicit == nil {
		t.Fatal("the question closed on a bad answer")
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"send","form":{"repo":"a/b","count":"3"}}`); f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	got := <-reply
	if got.Action != mcp.ElicitAccept || got.Content["repo"] != "a/b" || got.Content["count"] != 3 {
		t.Errorf("answer = %+v", got)
	}
	if m.confirmOpen || m.aiS().mcpx.elicit != nil {
		t.Error("the laptop's dialog stayed open")
	}
	if a := webElicitAuditArgs(&aiMCPElicitPending{req: mcp.ElicitRequest{Server: "gh", Fields: []mcp.ElicitField{{Name: "repo"}}}}); a != "server=gh fields=repo" {
		t.Errorf("audit = %q", a)
	}
}

// Deny declines; a secret-looking question has no form and no Send from
// the phone (Fill in on laptop only).
func TestWebElicitDenyAndSecret(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	reply := webTestElicit(&m, mcp.ElicitRequest{Server: "db", Mode: mcp.ElicitForm, Fields: []mcp.ElicitField{{Name: "password"}}})
	c := webTestSub(t, &m, "cards")
	cards := webTestCards(t, c)
	if len(cards) != 1 || len(cards[0].Form) != 0 || !strings.Contains(cards[0].Body, "laptop") {
		t.Fatalf("secret card = %+v", cards)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"send","form":{"password":"x"}}`); f["code"] != "forbidden" {
		t.Fatalf("send on a secret card: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpCardsAnswer, `{"id":"`+cards[0].ID+`","choice":"deny"}`); f["t"] != "res" {
		t.Fatalf("deny: %v", f)
	}
	if got := <-reply; got.Action != mcp.ElicitDecline || got.Content != nil {
		t.Errorf("answer = %+v", got)
	}
}
