package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestChatDeltas decodes the chat deltas among frames.
func webTestChatDeltas(t *testing.T, frames []map[string]any) []webproto.ChatDelta {
	t.Helper()
	var out []webproto.ChatDelta
	for _, f := range frames {
		if f["t"] != "delta" || f["topic"] != "chat" {
			continue
		}
		var d webproto.ChatDelta
		webTestData(t, f, &d)
		out = append(out, d)
	}
	return out
}

// webTestChatModel is a model with an open chat.
func webTestChatModel(t *testing.T, msgs ...ai.StoredMessage) Model {
	t.Helper()
	m := aiTestModel(t)
	m.ai.conv = ai.Conversation{ID: "c-1", Title: "Fix the build", Messages: msgs}
	return m
}

// Spec (WEB-13): the chat snapshot is the open chat, trimmed: user and
// answer text as markdown ASTs, tool and error lines as plain text, image
// names without folders, the run and its steps; nothing else of the
// stored message (no attached file context).
func TestChatSnapshotFromStore(t *testing.T) {
	m := webTestChatModel(t,
		ai.StoredMessage{Role: ai.StoredUser, Text: "Fix **it**", Context: "SECRET FILE BODY", Images: []string{"/home/u/shots/a.png"}, Rules: "AGENTS.md"},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read a.go → 10 lines"},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: "Done:\n\n```go\nx := 1\n```", Model: "small"},
		ai.StoredMessage{Role: ai.StoredError, Text: "rate limited"},
		ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1", State: ai.RunStateDone, Spent: 0.25},
			Steps: []ai.StoredStep{{ID: "s1", Title: "Plan", State: "done", Text: "# Steps", Tools: []string{"⚙ Read"}}}},
	)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	var s webproto.ChatSnap
	webTestData(t, webTestNow(t, c)[0], &s)
	if s.ID != "c-1" || s.Title != "Fix the build" || s.Busy || len(s.Messages) != 5 {
		t.Fatalf("snap %+v", s)
	}
	u := s.Messages[0]
	if u.MD == nil || u.Text != "" || u.Rules != "AGENTS.md" || len(u.Images) != 1 || u.Images[0] != "a.png" {
		t.Errorf("user %+v", u)
	}
	if tl := s.Messages[1]; tl.Role != "tool" || tl.Text != "⚙ Read a.go → 10 lines" || tl.MD != nil || tl.I != 1 {
		t.Errorf("tool %+v", tl)
	}
	if a := s.Messages[2]; a.MD == nil || a.MD.C[1].K != "codeblock" || a.MD.C[1].Lang != "go" || a.Model != "small" {
		t.Errorf("answer %+v", a)
	}
	if r := s.Messages[4]; r.Run == nil || r.Run.CostUSD != 0.25 || len(r.Steps) != 1 || r.Steps[0].MD == nil || r.Steps[0].Tools[0] != "⚙ Read" {
		t.Errorf("run %+v", r)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "SECRET FILE BODY") || strings.Contains(string(raw), "/home/u") {
		t.Error("the snapshot leaks the attached context or a folder")
	}
}

// Spec (WEB-13): a streaming answer goes out as appends: those queued for
// a slow link merge into one; the finished answer is one message delta
// with its AST and a state delta (busy off); the deltas replay to the
// newest snapshot.
func TestStreamAppendCoalesced(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "hi"}, ai.StoredMessage{Role: ai.StoredAssistant})
	m.ai.streaming = true
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "chat", func() {
		for _, tok := range []string{"Hel", "lo ", "**wor", "ld**"} {
			m.ai.conv.Messages[1].Text += tok
			m.webAfterUpdate(nil)
		}
	})
	if len(c.Deltas) != 1 {
		t.Fatalf("deltas %d (%s), want one merged append", len(c.Deltas), c.Deltas)
	}
	var d webproto.ChatDelta
	_ = json.Unmarshal(c.Deltas[0], &d)
	if d.Op != "append" || *d.I != 1 || d.Text != "Hello **world**" {
		t.Fatalf("append %+v", d)
	}
	webReplayCheck(t, h.topics["chat"], c)
	webReplayGolden(t, webReplayPath("chat", "stream_append"), c)

	sub := webTestSub(t, &m, "chat")
	webTestNow(t, sub)
	m.ai.conv.Messages[1].Text += "!"
	m.webAfterUpdate(nil)
	ds := webTestChatDeltas(t, webTestNow(t, sub))
	if len(ds) != 1 || ds[0].Text != "!" {
		t.Fatalf("one more token: %+v", ds)
	}
	m.ai.streaming = false
	m.webAfterUpdate(nil)
	ds = webTestChatDeltas(t, webTestNow(t, sub))
	if len(ds) != 2 || ds[0].Op != "message" || ds[0].Msg.MD == nil || ds[0].Msg.Streaming || ds[1].Op != "state" || ds[1].Busy == nil || *ds[1].Busy {
		t.Fatalf("finished: %+v", ds)
	}
	m.webAfterUpdate(nil)
	if ds := webTestChatDeltas(t, webTestNow(t, sub)); len(ds) != 0 {
		t.Fatalf("no change: %+v", ds)
	}
}

// Spec (WEB-13, T43): a registered secret streamed in pieces never leaves
// the hub, not even its start: the end that could begin a secret waits
// for the next token, and the whole secret is redacted.
func TestChatStreamHoldsSecretBack(t *testing.T) {
	secret := "zx" + "-chat-stream-secret-4417"
	ai.RegisterSecret(secret)
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "key?"}, ai.StoredMessage{Role: ai.StoredAssistant})
	m.ai.streaming = true
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	var stream, all strings.Builder // the answer as the page builds it; every frame
	frames := func() {
		got := webTestNow(t, c)
		for _, f := range got {
			b, _ := json.Marshal(f)
			all.Write(b)
		}
		for _, d := range webTestChatDeltas(t, got) {
			if d.Op == "append" {
				stream.WriteString(d.Text)
			}
		}
	}
	frames() // the snap
	text := "The key is " + secret + " and that is all."
	for i := 0; i < len(text); i += 3 {
		m.ai.conv.Messages[1].Text = text[:min(i+3, len(text))]
		m.webAfterUpdate(nil)
		frames()
	}
	if strings.Contains(stream.String(), secret[:4]) {
		t.Fatalf("a piece of the secret left the hub: %q", stream.String())
	}
	if !strings.HasPrefix(stream.String(), "The key is [REDACTED] and that") {
		t.Fatalf("streamed %q", stream.String())
	}
	m.ai.streaming = false
	m.webAfterUpdate(nil)
	frames()
	out := all.String()
	if strings.Contains(out, secret[:4]) {
		t.Fatalf("a piece of the secret left the hub: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") || !strings.Contains(out, "and that is all.") {
		t.Fatalf("the answer is missing: %s", out)
	}
}

// Spec (WEB-13): the laptop opening another chat is one switched delta
// with all of it; removed messages are a truncate; a title change is a
// state delta.
func TestChatSwitchedAndTruncate(t *testing.T) {
	m := webTestChatModel(t, ai.StoredMessage{Role: ai.StoredUser, Text: "a"}, ai.StoredMessage{Role: ai.StoredAssistant, Text: "b"})
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "chat", func() {
		m.ai.conv.Messages = m.ai.conv.Messages[:1]
		m.ai.conv.Title = "Renamed"
		m.webAfterUpdate(nil)
		m.ai.conv = ai.Conversation{ID: "c-2", Title: "Other", Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "x"}}}
		m.webAfterUpdate(nil)
	})
	var ops []string
	for _, raw := range c.Deltas {
		var d webproto.ChatDelta
		_ = json.Unmarshal(raw, &d)
		ops = append(ops, d.Op)
	}
	if strings.Join(ops, ",") != "truncate,state,switched" {
		t.Fatalf("ops %v", ops)
	}
	webReplayCheck(t, h.topics["chat"], c)
	webReplayGolden(t, webReplayPath("chat", "truncate_rename_switch"), c)
}

// Spec (WEB-13): a long chat's snap carries the newest messages whole, as
// many as fit 256 KiB of text; older ones are stubs (role, index, cut)
// the page fetches with chat.get_message.
func TestChatSnapCapsOldMessages(t *testing.T) {
	big := strings.Repeat("word ", 12<<10) // 60 KiB
	var msgs []ai.StoredMessage
	for i := 0; i < 10; i++ {
		msgs = append(msgs, ai.StoredMessage{Role: ai.StoredAssistant, Text: big})
	}
	m := webTestChatModel(t, msgs...)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "chat")
	f := webTestNow(t, c)[0]
	var s webproto.ChatSnap
	webTestData(t, f, &s)
	if len(s.Messages) != 10 {
		t.Fatalf("messages %d", len(s.Messages))
	}
	whole := 0
	for i, cm := range s.Messages {
		if cm.MD != nil {
			whole++
		} else if !cm.Truncated || cm.I != i || cm.Role != "assistant" {
			t.Errorf("stub %d: %+v", i, cm)
		}
	}
	if whole != 4 || s.Messages[9].MD == nil || s.Messages[0].MD != nil {
		t.Fatalf("%d whole messages, want the newest 4", whole)
	}
	if b, _ := json.Marshal(f); len(b) > 1<<20 {
		t.Fatalf("snap frame is %d bytes", len(b))
	}
}
