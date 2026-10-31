package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// compactFake answers like scripted replies; it records every request.
type compactFake struct {
	mu      sync.Mutex
	replies []ai.Chunk
	seen    []ai.Request
}

func (*compactFake) Name() string        { return "fake" }
func (*compactFake) Configured() bool    { return true }
func (*compactFake) SupportsTools() bool { return true }
func (f *compactFake) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	f.mu.Lock()
	r := ai.Chunk{Delta: "ok"}
	if n := len(f.seen); n < len(f.replies) {
		r = f.replies[n]
	}
	f.seen = append(f.seen, req)
	f.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: r.Delta}
	ch <- ai.Chunk{Done: true, Err: r.Err}
	close(ch)
	return ch
}

func (f *compactFake) requests() []ai.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ai.Request(nil), f.seen...)
}

// runAITurn sends text and feeds every message back until the turn ends.
func runAITurn(t *testing.T, m *Model, text string) {
	t.Helper()
	cmd := m.sendAIChat(text)
	for i := 0; cmd != nil && i < 200; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		if b, ok := msg.(tea.BatchMsg); ok && len(b) == 0 {
			break
		}
		next, ok := m.handleAIMsg(msg)
		if !ok {
			t.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			break
		}
		cmd = next
	}
	if m.ai.streaming {
		t.Fatal("still streaming")
	}
}

func TestAIChatHistoryStartsAtTheLastSummary(t *testing.T) {
	m := aiTestModel(t)
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "old question"},
		{Role: ai.StoredAssistant, Text: "old answer"},
		{Role: ai.StoredUser, Text: "new question", Summary: "We talked about a.go.", Summarised: 2},
		{Role: ai.StoredAssistant, Text: "new answer"},
	}
	h := m.aiChatHistory("sys")
	var all []string
	for _, x := range h {
		all = append(all, x.Content)
	}
	got := strings.Join(all, "|")
	if strings.Contains(got, "old question") || !strings.Contains(got, "We talked about a.go.") || !strings.HasSuffix(got, "new question|new answer") {
		t.Fatalf("history %q", got)
	}
	if h[0].Content != "sys" || h[1].Role != ai.RoleUser || h[2].Role != ai.RoleAssistant {
		t.Fatalf("roles %+v", h)
	}
}

func TestAIChatCompactsWhenTheContextIsTooLong(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	tooLong := ai.Chunk{Err: aiTestStatusErr(t)}
	main := &compactFake{replies: []ai.Chunk{tooLong, {Delta: "fixed"}}}
	quick := &compactFake{replies: []ai.Chunk{{Delta: "Summary: we read a.go."}}}
	s.provider = main
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	s.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "read a.go"}, {Role: ai.StoredAssistant, Text: "long text"}}
	runAITurn(t, &m, "now fix it")
	msgs := s.conv.Messages
	if len(msgs) != 4 || msgs[2].Summary != "Summary: we read a.go." || msgs[2].Summarised != 2 || msgs[3].Text != "fixed" {
		t.Fatalf("transcript %+v", msgs)
	}
	if q := quick.requests(); len(q) != 1 || !strings.Contains(q[0].Messages[1].Content, "read a.go") || q[0].Model != "quick-1" {
		t.Fatalf("quick requests %+v", q)
	}
	got, _ := s.store.Load(s.conv.ID)
	if got.Messages[2].Summary == "" {
		t.Fatal("summary not saved")
	}
}

func TestAIChatCompactsBeforeTheWindowIsFull(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	main := &compactFake{}
	quick := &compactFake{replies: []ai.Chunk{{Delta: "short summary"}}}
	s.provider = main
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "tiny-1"}
	s.settings.ContextWindows = map[string]int{"tiny-1": 4000}
	s.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "q"}, {Role: ai.StoredAssistant, Text: strings.Repeat("long ", 4000)}}
	runAITurn(t, &m, "next")
	r := main.requests()
	if len(r) != 1 || strings.Contains(r[0].Messages[1].Content, "long long") || !strings.Contains(r[0].Messages[1].Content, "short summary") {
		t.Fatalf("main got %d requests: %+v", len(r), r)
	}
	if s.conv.Messages[2].Summary != "short summary" {
		t.Fatalf("no marker: %+v", s.conv.Messages[2])
	}
}

func TestAIChatCompactOffSendsNoSummaryRequest(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	main := &compactFake{replies: []ai.Chunk{{Err: aiTestStatusErr(t)}}}
	quick := &compactFake{}
	s.provider = main
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "tiny-1"}
	s.settings.Compact = "off"
	s.settings.ContextWindows = map[string]int{"tiny-1": 4000}
	s.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "q"}, {Role: ai.StoredAssistant, Text: strings.Repeat("long ", 4000)}}
	runAITurn(t, &m, "next")
	if len(quick.requests()) != 0 || s.conv.Messages[2].Summary != "" {
		t.Fatal("compacted with ai_compact off")
	}
}

func TestAIChatShowsTheSummaryMarker(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "a"}, {Role: ai.StoredAssistant, Text: "b"},
		{Role: ai.StoredUser, Text: "c", Summary: "s", Summarised: 2},
	}
	var all []string
	for _, l := range m.buildAITranscript(60) {
		all = append(all, l.text)
	}
	if got := strings.Join(all, "\n"); !strings.Contains(got, "Earlier chat summarised (2 messages)") {
		t.Fatalf("no marker in:\n%s", got)
	}
}

func aiTestStatusErr(t *testing.T) error {
	t.Helper()
	err := ai.HTTPStatusError(400, `{"error":{"message":"prompt is too long: 300000 tokens"}}`)
	if !ai.ContextTooLong(err) {
		t.Fatal("fake error is not too long")
	}
	return err
}
