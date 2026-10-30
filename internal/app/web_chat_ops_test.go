package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestCall runs one request of browser c on the Model, as Update
// would, and returns its answer frame (waiting for one that comes later)
// and the op's Cmd.
func webTestCall(t *testing.T, m *Model, c *webConn, op, args string) (map[string]any, tea.Cmd) {
	t.Helper()
	webTestNow(t, c) // what was queued before
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
	cmd := m.webRunOp(fn, call)
	for _, f := range webTestTake(t, c) {
		if f["id"] == float64(call.id) {
			return f, cmd
		}
	}
	t.Fatalf("%s gave no answer", op)
	return nil, nil
}

// webTestFakeAI configures a provider that answers reply.
func webTestFakeAI(m *Model, reply string) {
	m.ai.provider = fakeAIProvider{reply: reply}
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
}

// Spec (WEB-13): chat.send types into the laptop's open chat; the fake
// provider's answer streams to the page as chat deltas and ends with the
// whole message and busy off. While it answers a second send is busy;
// a send to another chat is a conflict; an empty one bad_request.
func TestChatSendStreamsToPage(t *testing.T) {
	m := webTestChatModel(t)
	webTestFakeAI(&m, "Hello **web**")
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	res, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"hi from the phone"}`)
	if res["t"] != "res" || res["data"].(map[string]any)["id"] != "c-1" {
		t.Fatalf("send: %v", res)
	}
	if !m.ai.streaming || m.ai.conv.Messages[0].Text != "hi from the phone" {
		t.Fatalf("not sent: %+v", m.ai.conv.Messages)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"again"}`); f["code"] != "busy" {
		t.Fatalf("second send: %v", f)
	}
	var deltas []webproto.ChatDelta
	for i := 0; i < 50 && m.ai.chatCh != nil; i++ {
		msg := nextAIMsg(t, waitAIChat(m.ai.chatCh))
		if msg == nil {
			break
		}
		m.handleAIMsg(msg)
		m.webAfterUpdate(msg)
		deltas = append(deltas, webTestChatDeltas(t, webTestNow(t, c))...)
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
	}
	if len(deltas) < 3 {
		t.Fatalf("deltas %+v", deltas)
	}
	last, state := deltas[len(deltas)-2], deltas[len(deltas)-1]
	if last.Op != "message" || last.Msg.MD == nil || last.Msg.Role != "assistant" || state.Op != "state" || *state.Busy {
		t.Fatalf("end of the turn: %+v %+v", last, state)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"x","chat_id":"c-other"}`); f["code"] != "conflict" {
		t.Fatalf("other chat: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"   "}`); f["code"] != "bad_request" {
		t.Fatalf("empty: %v", f)
	}
}

// Spec (WEB-13): chat.stop stops the turn (ok even when idle); chat.new
// opens an empty chat without moving the laptop's focus, and is busy while
// the AI answers.
func TestChatStopAndNew(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "a"})
	webTestFakeAI(&m, "slow")
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	stopped := false
	m.ai.streaming = true
	m.ai.chatCancel = func() { stopped = true }
	if f, _ := webTestCall(t, &m, c, webproto.OpChatNew, `{}`); f["code"] != "busy" {
		t.Fatalf("new while busy: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatStop, `{}`); f["t"] != "res" || !stopped {
		t.Fatalf("stop: %v stopped=%v", f, stopped)
	}
	m.ai.streaming, m.ai.chatCancel = false, nil
	if f, _ := webTestCall(t, &m, c, webproto.OpChatStop, `{}`); f["t"] != "res" {
		t.Fatalf("stop when idle: %v", f)
	}
	m.ai.focused = false
	f, _ := webTestCall(t, &m, c, webproto.OpChatNew, `{}`)
	if f["t"] != "res" || m.ai.conv.ID == "c-1" || len(m.ai.conv.Messages) != 0 || m.ai.focused {
		t.Fatalf("new: %v id=%s focused=%v", f, m.ai.conv.ID, m.ai.focused)
	}
	if f["data"].(map[string]any)["id"] != m.ai.conv.ID {
		t.Fatalf("new answers with the new chat: %v", f)
	}
}

// Spec (WEB-13): chat.open answers with a stored chat (read off the UI
// thread) or the open one; a missing one is not_found. chat.get_message
// answers with a whole message beyond the 64 KiB cut; chat.pin is ok.
func TestChatOpenGetMessagePin(t *testing.T) {
	long := strings.Repeat("x", 80<<10)
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredTool, Text: long})
	webTestHub(t, &m)
	webTestStoreChat(t, m.ai.store, "c-old", "Old chat", 1000)
	c := webTestSub(t, &m)
	f, _ := webTestCall(t, &m, c, webproto.OpChatOpen, `{"id":"c-old"}`)
	var s webproto.ChatSnap
	webTestData(t, f, &s)
	if s.ID != "c-old" || len(s.Messages) != 1 || s.Messages[0].MD == nil {
		t.Fatalf("open stored: %v", f)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpChatOpen, `{"id":"c-1"}`)
	webTestData(t, f, &s)
	if s.ID != "c-1" || !s.Messages[0].Truncated || len(s.Messages[0].Text) != webproto.MaxChatMsgText {
		t.Fatalf("open the laptop's: %+v", s.Messages[0].Truncated)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatOpen, `{"id":"c-none"}`); f["code"] != "not_found" {
		t.Fatalf("missing: %v", f)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpChatGetMessage, `{"i":0}`)
	var cm webproto.ChatMsg
	webTestData(t, f, &cm)
	if len(cm.Text) != len(long) || cm.Truncated {
		t.Fatalf("get_message: %d bytes, truncated=%v", len(cm.Text), cm.Truncated)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatGetMessage, `{"i":5}`); f["code"] != "not_found" {
		t.Fatalf("out of range: %v", f)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatPin, `{"on":true}`); f["t"] != "res" {
		t.Fatalf("pin: %v", f)
	}
}
