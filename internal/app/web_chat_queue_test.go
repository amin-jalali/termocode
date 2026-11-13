package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

func webTestQueue(t *testing.T, f map[string]any) []any {
	t.Helper()
	if f["t"] != "res" {
		t.Fatalf("queue: %v", f)
	}
	q, _ := f["data"].(map[string]any)["queue"].([]any)
	return q
}

func webTestQueueID(q []any, i int) string {
	return q[i].(map[string]any)["id"].(string)
}

func lastUserText(m *Model) string {
	msgs := m.ai.conv.Messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == ai.StoredUser {
			return msgs[i].Text
		}
	}
	return ""
}

// Spec (CMP-46): while the laptop's AI answers, chat.queue lines a
// message up instead of busy: it is not sent, the chat topic shows the
// queue, at most 5 wait (the 6th is busy). When the turn ends the first
// queued message is sent in the same chat, as if typed there.
func TestChatQueueSendsWhenTheTurnEnds(t *testing.T) {
	m := webTestChatModel(t)
	webTestFakeAI(&m, "ok")
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	if f, _ := webTestCall(t, &m, c, webproto.OpChatSend, `{"text":"first"}`); f["t"] != "res" || !m.ai.streaming {
		t.Fatalf("send: %v", f)
	}
	f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"then the docs"}`)
	if q := webTestQueue(t, f); len(q) != 1 || q[0].(map[string]any)["text"] != "then the docs" {
		t.Fatalf("queued: %v", q)
	}
	if lastUserText(&m) != "first" {
		t.Fatalf("sent at once: %+v", m.ai.conv.Messages)
	}
	m.webAfterUpdate(nil)
	var seen bool
	for _, d := range webTestChatDeltas(t, webTestNow(t, c)) {
		if d.Op == webproto.ChatState && d.Queue != nil && len(d.Queue.Queue) == 1 {
			seen = true
		}
	}
	if !seen {
		t.Fatal("the chat topic does not show the queue")
	}
	var ids []string
	for i := 0; i < 4; i++ {
		f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"more"}`)
		q := webTestQueue(t, f)
		ids = append(ids, webTestQueueID(q, len(q)-1))
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"one too many"}`); f["code"] != "busy" {
		t.Fatalf("6th: %v", f)
	}
	for _, id := range ids {
		if f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"remove":"`+id+`"}`); f["t"] != "res" {
			t.Fatalf("remove %s: %v", id, f)
		}
	}
	for i := 0; i < 50 && m.ai.chatCh != nil; i++ {
		msg := nextAIMsg(t, waitAIChat(m.ai.chatCh))
		if msg == nil {
			break
		}
		m.handleAIMsg(msg)
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
	}
	if lastUserText(&m) != "then the docs" || !m.ai.streaming || len(m.ai.queue) != 0 {
		t.Fatalf("queued message not sent at the end of the turn: %q streaming=%v queue=%v", lastUserText(&m), m.ai.streaming, m.ai.queue)
	}
}

// Spec (CMP-46): {now} steers: the turn stops and that message goes next
// (the rest keep waiting after it). A plain Stop (chat.stop) also drops
// the queue, so nothing is sent behind the user's back. While the AI is
// idle a queued message is sent at once. A message that already went is
// not_found.
func TestChatQueueNowSteersAndStopDropsIt(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "a"})
	webTestFakeAI(&m, "ok")
	webTestHub(t, &m)
	c := webTestSub(t, &m)
	stopped := 0
	m.ai.streaming = true
	m.ai.chatCancel = func() { stopped++ }
	webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"docs too"}`)
	f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"no, use the user id"}`)
	steer := webTestQueueID(webTestQueue(t, f), 1)
	f, _ = webTestCall(t, &m, c, webproto.OpChatQueue, `{"now":"`+steer+`"}`)
	q := webTestQueue(t, f)
	if stopped != 1 || len(q) != 2 || webTestQueueID(q, 0) != steer {
		t.Fatalf("steer: stopped=%d queue=%v", stopped, q)
	}
	m.ai.streaming, m.ai.chatCancel = false, nil // the stopped turn ended
	m.aiSendQueued()
	if lastUserText(&m) != "no, use the user id" || len(m.ai.queue) != 1 {
		t.Fatalf("steered message: %q queue=%v", lastUserText(&m), m.ai.queue)
	}
	m.ai.chatCancel = func() {}
	if f, _ := webTestCall(t, &m, c, webproto.OpChatStop, `{}`); f["t"] != "res" || len(m.ai.queue) != 0 {
		t.Fatalf("stop kept the queue: %v", m.ai.queue)
	}
	m.ai.streaming, m.ai.chatCancel = false, nil
	if f, _ := webTestCall(t, &m, c, webproto.OpChatQueue, `{"remove":"`+steer+`"}`); f["code"] != "not_found" {
		t.Fatalf("remove a sent one: %v", f)
	}
	f, _ = webTestCall(t, &m, c, webproto.OpChatQueue, `{"text":"idle now"}`)
	if q := webTestQueue(t, f); len(q) != 0 || lastUserText(&m) != "idle now" || !m.ai.streaming {
		t.Fatalf("idle queue: %v %q", q, lastUserText(&m))
	}
}
