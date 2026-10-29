package ai

import "testing"

func forkSrc() Conversation {
	return Conversation{
		ID:    "orig",
		Title: "Parser tests",
		Messages: []StoredMessage{
			{Role: StoredUser, Text: "first question", Images: []string{"a.png"}},
			{Role: StoredTool, Text: "⚙ Read a.go"},
			{Role: StoredAssistant, Text: "first answer", Model: "m1"},
			{Role: StoredUser, Text: "second question"},
			{Role: StoredAssistant, Text: "second answer", Run: &StoredRun{ID: "r", State: RunStateDone}, Steps: []StoredStep{{ID: "s1", Tools: []string{"x"}}}},
		},
		Folded: map[string]bool{"step:r:s1": true},
	}
}

// G-FORK: a fork of an AI message keeps the history up to and with it;
// the input stays empty.
func TestForkConversationAtAnswer(t *testing.T) {
	src := forkSrc()
	f, input, ok := ForkConversation(src, 2)
	if !ok {
		t.Fatal("fork refused")
	}
	if input != "" {
		t.Errorf("input %q, want empty", input)
	}
	if len(f.Messages) != 3 || f.Messages[2].Text != "first answer" || f.Messages[2].Model != "m1" {
		t.Fatalf("messages %+v", f.Messages)
	}
	if f.ID == "" || f.ID == src.ID {
		t.Errorf("fork id %q", f.ID)
	}
	if f.Title != "Parser tests (fork)" {
		t.Errorf("title %q", f.Title)
	}
	if f.ForkedFrom == nil || f.ForkedFrom.ID != "orig" || f.ForkedFrom.Title != "Parser tests" || f.ForkedFrom.Message != 2 {
		t.Errorf("forked from %+v", f.ForkedFrom)
	}
	if got := f.ForkNote(); got != "Forked from Parser tests at message 2" {
		t.Errorf("note %q", got)
	}
}

// A fork of a user message keeps the history before it and gives the
// message text back for the input box (not resent).
func TestForkConversationAtQuestion(t *testing.T) {
	f, input, ok := ForkConversation(forkSrc(), 3)
	if !ok || input != "second question" {
		t.Fatalf("ok %v input %q", ok, input)
	}
	if len(f.Messages) != 3 || f.Messages[2].Role != StoredAssistant {
		t.Fatalf("messages %+v", f.Messages)
	}
	if f.ForkedFrom.Message != 3 {
		t.Errorf("message number %d", f.ForkedFrom.Message)
	}
}

// The original is never changed: the fork holds its own copies.
func TestForkConversationCopies(t *testing.T) {
	src := forkSrc()
	f, _, _ := ForkConversation(src, 4)
	f.Messages[0].Images[0] = "changed.png"
	f.Messages[4].Steps[0].Tools[0] = "changed"
	f.Messages[4].Run.State = RunStateFailed
	f.Folded["step:r:s1"] = false
	if src.Messages[0].Images[0] != "a.png" || src.Messages[4].Steps[0].Tools[0] != "x" ||
		src.Messages[4].Run.State != RunStateDone || !src.Folded["step:r:s1"] {
		t.Fatal("fork shares memory with the original")
	}
	if len(src.Messages) != 5 || src.Title != "Parser tests" || src.ForkedFrom != nil {
		t.Fatal("original changed")
	}
}

// Bad indexes and tool lines: out of range is refused; a tool line
// forks like an answer (its turn so far).
func TestForkConversationEdges(t *testing.T) {
	if _, _, ok := ForkConversation(forkSrc(), -1); ok {
		t.Error("negative index forked")
	}
	if _, _, ok := ForkConversation(forkSrc(), 5); ok {
		t.Error("index past the end forked")
	}
	f, input, ok := ForkConversation(forkSrc(), 1)
	if !ok || input != "" || len(f.Messages) != 2 {
		t.Fatalf("tool line fork: ok %v input %q n %d", ok, input, len(f.Messages))
	}
	// A chat with the default title takes its first question.
	src := forkSrc()
	src.Title = ""
	f, _, _ = ForkConversation(src, 2)
	if f.Title != "first question (fork)" {
		t.Errorf("title %q", f.Title)
	}
	// A fork of a fork does not stack "(fork) (fork)".
	g, _, _ := ForkConversation(f, 0)
	if g.Title != "first question (fork)" {
		t.Errorf("fork of fork title %q", g.Title)
	}
}

// The fork survives a save and load with its note.
func TestForkConversationSaved(t *testing.T) {
	s := ChatStore{Dir: t.TempDir()}
	f, _, _ := ForkConversation(forkSrc(), 2)
	if err := s.Save(&f); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(f.ID)
	if err != nil || got.ForkedFrom == nil || got.ForkNote() != f.ForkNote() || got.Title != f.Title {
		t.Fatalf("loaded %+v %v", got, err)
	}
}
