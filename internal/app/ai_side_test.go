package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

func sideModel(t *testing.T) (Model, *compactFake) {
	m := aiTestModel(t)
	s := m.ai
	s.provider = fakeAIProvider{reply: "main"}
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	quick := &compactFake{replies: []ai.Chunk{{Delta: "Use errors.Is."}, {Delta: "Yes, wrap it."}}}
	s.slotFake = map[string]aiSlot{ai.SlotQuick: {p: quick, cfg: ai.Config{Kind: ai.KindOllama, Model: "quick-1"}}}
	s.conv.Messages = []ai.StoredMessage{{Role: ai.StoredUser, Text: "refactor the errors"}, {Role: ai.StoredAssistant, Text: "working on errors.go"}}
	return m, quick
}

func TestAISideChatAnswersBesideAStreamingTurn(t *testing.T) {
	m, quick := sideModel(t)
	s := m.ai
	s.streaming = true // a run streams in the main chat
	m.openAISide()
	if !m.promptOpen || m.promptKind != promptKindAISide {
		t.Fatal("no side prompt")
	}
	m.promptOpen = false
	work := m.onAISideQuestion("how do I compare errors?")
	if work == nil {
		t.Fatal("no side request")
	}
	msg := runSideWork(t, work)
	m.handleAIMsg(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindAISide {
		t.Fatal("no side card")
	}
	if len(s.conv.Messages) != 2 {
		t.Fatalf("the side chat went into the main chat: %+v", s.conv.Messages)
	}
	q := quick.requests()
	if len(q) != 1 || !strings.Contains(q[0].Messages[0].Content, "working on errors.go") || q[0].Messages[len(q[0].Messages)-1].Content != "how do I compare errors?" || len(q[0].Tools) != 0 {
		t.Fatalf("side request %+v", q)
	}
	if b := m.confirm.Buttons(); len(b) < 2 || b[0].ID != "close" {
		t.Fatalf("buttons %+v", b)
	}
	// A follow-up keeps the side history, not the main chat's.
	m.confirmOpen = false
	m.handleAIMsg(runSideWork(t, m.onAISideQuestion("and wrapping?")))
	q = quick.requests()
	last := q[1].Messages
	if len(last) != 4 || last[1].Content != "how do I compare errors?" || last[2].Content != "Use errors.Is." {
		t.Fatalf("follow-up %+v", last)
	}
}

func TestAISideAnswerWaitsWhenACardIsOpen(t *testing.T) {
	m, _ := sideModel(t)
	work := m.onAISideQuestion("q")
	m.confirmOpen, m.confirmKind = true, confirmKindAITool // the main run asks to allow a tool
	m.handleAIMsg(runSideWork(t, work))
	if m.confirmKind != confirmKindAITool {
		t.Fatal("the side answer replaced the tool card")
	}
	m.confirmOpen = false
	m.openAISide()
	if !m.confirmOpen || m.confirmKind != confirmKindAISide {
		t.Fatal("the waiting answer is not shown")
	}
}

func runSideWork(t *testing.T, work tea.Cmd) aiSideDoneMsg {
	t.Helper()
	if work == nil {
		t.Fatal("no side request")
	}
	msg, ok := work().(aiSideDoneMsg)
	if !ok {
		t.Fatal("not a side answer")
	}
	return msg
}
