package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// runAIChain sends text and drives every turn the keep-going loop starts
// after it.
func runAIChain(t *testing.T, m *Model, text string) {
	t.Helper()
	driveAITurn(t, m, m.sendAIChat(text))
	for i := 0; m.ai.streaming && i < 20; i++ {
		driveAITurn(t, m, waitAIChat(m.ai.chatCh))
	}
	if m.ai.streaming {
		t.Fatal("still streaming")
	}
}

func aiUserTexts(s *aiState) []string {
	var out []string
	for _, msg := range s.conv.Messages {
		if msg.Role == ai.StoredUser {
			out = append(out, msg.Text)
		}
	}
	return out
}

func keepGoingModel(t *testing.T, replies ...ai.Chunk) (Model, *compactFake) {
	m := aiTestModel(t)
	p := &compactFake{replies: replies}
	m.ai.provider = p
	m.ai.cfg = ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	m.ai.conv.KeepGoing = true
	return m, p
}

func TestAIKeepGoingPushesWhileTodosAreOpen(t *testing.T) {
	m, _ := keepGoingModel(t,
		todoCall("1", [2]string{"a", "pending"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "started"},
		todoCall("2", [2]string{"a", "done"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "a is done"},
		todoCall("3", [2]string{"a", "done"}, [2]string{"b", "done"}), ai.Chunk{Delta: "all done"},
		ai.Chunk{Delta: "never sent"},
	)
	runAIChain(t, &m, "do a and b")
	users := aiUserTexts(m.ai)
	if len(users) != 3 || !strings.Contains(users[1], "2 todos are still open") || !strings.Contains(users[2], "1 todo is still open") {
		t.Fatalf("user messages %q", users)
	}
	var pushes []int
	for _, msg := range m.ai.conv.Messages {
		if msg.Role == ai.StoredUser {
			pushes = append(pushes, msg.KeepGoing)
		}
	}
	if pushes[0] != 0 || pushes[1] != 1 || pushes[2] != 2 {
		t.Fatalf("push numbers %v", pushes)
	}
	if got := aiTranscriptText(&m, 80); !strings.Contains(got, "keep going 1/3") {
		t.Fatalf("no auto chip:\n%s", got)
	}
}

func TestAIKeepGoingIsOffByDefault(t *testing.T) {
	m, _ := keepGoingModel(t, todoCall("1", [2]string{"a", "pending"}), ai.Chunk{Delta: "started"})
	m.ai.conv.KeepGoing = false
	runAIChain(t, &m, "do a")
	if n := len(aiUserTexts(m.ai)); n != 1 {
		t.Fatalf("%d user messages", n)
	}
}

func TestAIKeepGoingStopsAtTheRoundLimit(t *testing.T) {
	m, _ := keepGoingModel(t,
		todoCall("1", [2]string{"a", "pending"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "started"},
		todoCall("2", [2]string{"a", "done"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "a is done"},
	)
	m.ai.settings.UntilDoneRounds = 1
	runAIChain(t, &m, "do a and b")
	if n := len(aiUserTexts(m.ai)); n != 2 {
		t.Fatalf("%d user messages, want 2", n)
	}
	if got := aiTranscriptText(&m, 80); !strings.Contains(got, "stopped: 1 of 1 turns used") {
		t.Fatalf("no stop note:\n%s", got)
	}
}

func TestAIKeepGoingStopsWithoutProgress(t *testing.T) {
	m, _ := keepGoingModel(t,
		todoCall("1", [2]string{"a", "pending"}), ai.Chunk{Delta: "started"},
		ai.Chunk{Delta: "I did nothing"},
		ai.Chunk{Delta: "never"},
	)
	runAIChain(t, &m, "do a")
	if n := len(aiUserTexts(m.ai)); n != 2 {
		t.Fatalf("%d user messages, want 2", n)
	}
	if got := aiTranscriptText(&m, 80); !strings.Contains(got, "no progress") {
		t.Fatalf("no stop note:\n%s", got)
	}
}

func TestAIKeepGoingStopsAtTheBudget(t *testing.T) {
	use := &ai.Usage{Model: "fake-1", Input: 3}
	m, _ := keepGoingModel(t,
		todoCall("1", [2]string{"a", "pending"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "started"},
		todoCall("2", [2]string{"a", "done"}, [2]string{"b", "pending"}), ai.Chunk{Delta: "a is done", Usage: use},
		ai.Chunk{Delta: "never"},
	)
	m.ai.cfg.Kind = ai.KindOpenAI
	m.ai.settings.Prices = map[string]ai.Price{"fake-1": {In: 1e6, Out: 1e6}}
	runAIChain(t, &m, "do a and b")
	if n := len(aiUserTexts(m.ai)); n != 2 {
		t.Fatalf("%d user messages, want 2", n)
	}
	if got := aiTranscriptText(&m, 80); !strings.Contains(got, "budget") {
		t.Fatalf("no budget note:\n%s", got)
	}
}

// OMX-45: Stop ends the loop at once; the next message of the user
// starts it again.
func TestAIKeepGoingEndsOnStop(t *testing.T) {
	m, _ := keepGoingModel(t,
		todoCall("1", [2]string{"a", "pending"}), ai.Chunk{Delta: "started"},
	)
	cmd := m.sendAIChat("do a")
	m.aiCancelAll()
	driveAITurn(t, &m, cmd)
	if m.ai.streaming || len(aiUserTexts(m.ai)) != 1 {
		t.Fatalf("a stopped turn went on: %q", aiUserTexts(m.ai))
	}
	if m.ai.keep.stopped != true {
		t.Fatal("stop not kept")
	}
	m.ai.conv.Todos = []ai.Todo{{Text: "a", Status: ai.TodoPending}}
	m.ai.provider = &compactFake{replies: []ai.Chunk{todoCall("2", [2]string{"a", "pending"}, [2]string{"b", "pending"}), {Delta: "more"}, {Delta: "x"}}}
	runAIChain(t, &m, "go on")
	if n := len(aiUserTexts(m.ai)); n != 3 {
		t.Fatalf("the user's message did not start the loop again: %q", aiUserTexts(m.ai))
	}
}

func TestAIKeepGoingToggle(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	m.toggleAIKeepGoing()
	if !m.ai.conv.KeepGoing {
		t.Fatal("not on")
	}
	m.toggleAIKeepGoing()
	if m.ai.conv.KeepGoing {
		t.Fatal("not off")
	}
}

func TestAIKeepGoingPaletteEntry(t *testing.T) {
	m := aiTestModel(t)
	m.ai.provider = fakeAIProvider{}
	if cmd, ok := m.dispatchAIPalette("ai-keep-going"); !ok || cmd == nil || !m.ai.conv.KeepGoing {
		t.Fatal("palette entry does not toggle keep going")
	}
}
