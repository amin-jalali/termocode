//go:build unix

package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestCallLater runs op like webTestCall, for a handler that answers
// later: it returns the call and the handler's cmd, and no answer yet.
func webTestCallLater(t *testing.T, m *Model, c *webConn, op, args string) (*webCall, tea.Cmd) {
	t.Helper()
	webTestNow(t, c)
	a, err := webproto.DecodeArgs(op, json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s %s: %v", op, args, err)
	}
	c.lastID++
	call := &webCall{ctx: c.ctx, conn: c, id: c.lastID, op: op, args: a}
	c.inflight.Add(1)
	fn := m.web.ops[op]
	if fn == nil {
		t.Fatalf("no handler for %s", op)
	}
	return call, m.webRunOp(fn, call)
}

// webTestAnswer is call's answer among the queued frames (nil: none yet).
func webTestAnswer(t *testing.T, c *webConn, call *webCall) map[string]any {
	t.Helper()
	for _, f := range webTestNow(t, c) {
		if f["id"] == float64(call.id) {
			return f
		}
	}
	return nil
}

// Spec (WEBP-9, AI: Side Question…): chat.side asks the quick model a
// side question from the page, beside the main chat (even while it
// answers). The answer goes back to the page: it is never added to the
// main chat and opens no card on the laptop. Follow-ups keep the side
// history, the same one the laptop's side card shows. One question at a
// time (busy); a side question for a chat not open on the laptop is a
// conflict; the audit line names its size, never its text.
func TestWebChatSideAnswersThePage(t *testing.T) {
	m, quick := sideModel(t)
	m.output = newOutputStore()
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	m.ai.streaming = true // the main chat is answering
	call, work := webTestCallLater(t, &m, c, webproto.OpChatSide, `{"text":"how do I compare errors?"}`)
	if f := webTestAnswer(t, c, call); f != nil || work == nil {
		t.Fatalf("answered before the model: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSide, `{"text":"another"}`); webTestErr(f) != "busy" {
		t.Fatalf("second question: %v", f)
	}
	m.handleAIMsg(runSideWork(t, work))
	f := webTestAnswer(t, c, call)
	if f == nil || f["t"] != "res" || f["data"].(map[string]any)["answer"] != "Use errors.Is." {
		t.Fatalf("answer: %v", f)
	}
	if m.confirmOpen || len(m.ai.conv.Messages) != 2 {
		t.Fatalf("the side answer opened a card (%v) or went into the main chat: %+v", m.confirmOpen, m.ai.conv.Messages)
	}
	call, work = webTestCallLater(t, &m, c, webproto.OpChatSide, `{"text":"and wrapping?"}`)
	m.handleAIMsg(runSideWork(t, work))
	if f := webTestAnswer(t, c, call); f == nil || f["data"].(map[string]any)["answer"] != "Yes, wrap it." {
		t.Fatalf("follow-up: %v", f)
	}
	if q := quick.requests(); len(q) != 2 || len(q[1].Messages) != 4 || q[1].Messages[1].Content != "how do I compare errors?" {
		t.Fatalf("the follow-up lost the side history: %+v", q)
	}
	if len(m.ai.side.msgs) != 4 {
		t.Fatalf("the laptop's side history: %+v", m.ai.side.msgs)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSide, `{"text":"q","chat_id":"other"}`); webTestErr(f) != "conflict" {
		t.Fatalf("another chat: %v", f)
	}
	if a := webTestAudit(&m); strings.Contains(a, "compare errors") {
		t.Errorf("the audit has the question's text:\n%s", a)
	}
}
