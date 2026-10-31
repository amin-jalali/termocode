package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

const aiTestKey = "sk-" + "ant-test0123456789abcdefghijklmnop"

func saveTestChat(t *testing.T, s *aiState, title string, msgs ...ai.StoredMessage) ai.Conversation {
	t.Helper()
	c := ai.NewConversation()
	c.Messages = msgs
	if err := s.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	if title != "" {
		c.Title = title
		if err := s.store.Save(&c); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestAIChatsSearchFindsOldChatsRedacted(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	deploy := saveTestChat(t, s, "", ai.StoredMessage{Role: ai.StoredUser, Text: "how do we deploy the api?"},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: "Deploy with make ship, key " + aiTestKey})
	saveTestChat(t, s, "", ai.StoredMessage{Role: ai.StoredUser, Text: "rename a var"})
	tl, ok := ai.FindTool(m.aiSessionTools(), "chats_search")
	if !ok || tl.Mutating || tl.Class != "" {
		t.Fatal("chats_search missing or not read-only")
	}
	out, err := tl.Run(context.Background(), map[string]any{"query": "DEPLOY"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, deploy.ID) || strings.Contains(out, "rename a var") || strings.Contains(out, "0123456789abcdef") {
		t.Fatalf("search result:\n%s", out)
	}
	if out, _ := tl.Run(context.Background(), map[string]any{"query": "nothing-like-this"}); !strings.Contains(out, "no chat") {
		t.Fatalf("empty result %q", out)
	}
}

func TestAIChatReadReturnsAChatRedacted(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	c := saveTestChat(t, s, "", ai.StoredMessage{Role: ai.StoredUser, Text: "use key " + aiTestKey, Context: "Attached secret.txt: hidden"},
		ai.StoredMessage{Role: ai.StoredTool, Text: "⚙ Read a.go → 10 lines"},
		ai.StoredMessage{Role: ai.StoredAssistant, Text: "done"})
	tl, ok := ai.FindTool(m.aiSessionTools(), "chat_read")
	if !ok || tl.Mutating {
		t.Fatal("chat_read missing or mutating")
	}
	out, err := tl.Run(context.Background(), map[string]any{"id": c.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"User: use key", "Tool: ⚙ Read a.go", "Assistant: done"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "0123456789abcdef") || strings.Contains(out, "hidden") {
		t.Fatalf("secret or attachment in:\n%s", out)
	}
	if _, err := tl.Run(context.Background(), map[string]any{"id": "../../etc/passwd"}); err == nil {
		t.Fatal("bad id read")
	}
}

func TestAIHandoffWritesASummaryIntoANewChat(t *testing.T) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	quick := &compactFake{replies: []ai.Chunk{{Delta: "Goal: ship the cache."}}}
	s.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "build the cache"}, {Role: ai.StoredAssistant, Text: "half done"}}
	s.conv.Title = "build the cache"
	old := s.conv.ID
	work := m.aiHandoffCmd()
	if work == nil {
		t.Fatal("no handoff")
	}
	done, ok := work().(aiHandoffMsg)
	if !ok {
		t.Fatal("no handoff result")
	}
	m.handleAIMsg(done)
	if s.conv.ID == old || len(s.conv.Messages) != 0 {
		t.Fatalf("no new chat: %s %+v", s.conv.ID, s.conv.Messages)
	}
	in := string(s.input)
	if !strings.Contains(in, "Goal: ship the cache.") || !strings.Contains(in, "build the cache") {
		t.Fatalf("draft %q", in)
	}
	if got, err := s.store.Load(old); err != nil || len(got.Messages) != 2 {
		t.Fatal("the old chat is gone")
	}
	if q := quick.requests(); len(q) != 1 || !strings.Contains(q[0].Messages[1].Content, "half done") {
		t.Fatalf("quick requests %+v", q)
	}
}

func TestAIHandoffNeedsAChat(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	m.aiHandoff()
	if m.toast.Empty() {
		t.Fatal("no toast for an empty chat")
	}
}

func TestAIHandoffPaletteEntry(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	if cmd, ok := m.dispatchAIPalette("ai-handoff"); !ok || cmd == nil {
		t.Fatal("no palette entry")
	}
}
