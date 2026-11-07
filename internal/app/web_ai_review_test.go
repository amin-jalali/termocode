package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webReviewUserText is the text of the chat's last user message.
func webReviewUserText(s *aiState) string {
	for i := len(s.conv.Messages) - 1; i >= 0; i-- {
		if s.conv.Messages[i].Role == ai.StoredUser {
			return s.conv.Messages[i].Text
		}
	}
	return ""
}

// Spec (MOB-14): "Send N notes to agent" sends one chat message: the
// branch, then each note with its file, line and side, the line's text
// quoted from the agent's file, and the note. A file that may hold
// secrets is never quoted, and the note text is redacted.
func TestWebAIWorktreeNotesSendsOneMessage(t *testing.T) {
	r, _ := webAIWorktreeRig(t)
	notes := []webproto.ReviewNote{
		{Path: "a.txt", Line: 1, Side: "new", Text: "Why this word?"},
		{Path: "a.txt", Line: 1, Side: "old", Text: "Keep the old one."},
		{Path: ".env", Line: 1, Side: "new", Text: "Do not add this file. key sk-" + "live1234567890abcdef"},
	}
	var ref webproto.ChatRef
	webResData(t, r.call(webproto.OpAIWorktreeNotes, &webproto.AIWorktreeNotesArgs{ID: "old-x", Notes: notes}), &ref)
	s := r.m.aiS()
	if ref.ID != s.conv.ID {
		t.Errorf("ref %q, chat %q", ref.ID, s.conv.ID)
	}
	got := webReviewUserText(s)
	for _, want := range []string{
		git.AgentBranchPrefix + "old-x",
		"1. a.txt line 1 (after the agent)\n   > agent\n   Why this word?",
		"2. a.txt line 1 (before the agent)\n   > mine\n   Keep the old one.",
		"3. .env line 1 (after the agent)\n   (the file may hold secrets; the line is not quoted)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "KEY=") || strings.Contains(got, "sk-"+"test") || strings.Contains(got, "live1234567890abcdef") {
		t.Errorf("a secret reached the chat:\n%s", got)
	}
}

// Spec (MOB-14): a note on a file the agent did not change, or on a
// worktree that is gone, sends nothing; a line past the end is named,
// not quoted.
func TestWebAIWorktreeNotesRefusals(t *testing.T) {
	r, _ := webAIWorktreeRig(t)
	s := r.m.aiS()
	before := len(s.conv.Messages)
	one := func(id, path string, line int) map[string]any {
		return r.call(webproto.OpAIWorktreeNotes, &webproto.AIWorktreeNotesArgs{ID: id, Notes: []webproto.ReviewNote{
			{Path: "a.txt", Line: 1, Side: "new", Text: "ok"},
			{Path: path, Line: line, Side: "new", Text: "x"},
		}})
	}
	if code := webErrCode(one("old-x", "nope.txt", 1)); code != "not_found" {
		t.Errorf("unchanged file: %s", code)
	}
	if code := webErrCode(one("gone", "a.txt", 1)); code != "not_found" {
		t.Errorf("gone worktree: %s", code)
	}
	if len(s.conv.Messages) != before {
		t.Fatalf("a refused send reached the chat: %d messages", len(s.conv.Messages))
	}
	if f := one("old-x", "a.txt", 9); f["t"] != "res" || !strings.Contains(webReviewUserText(s), "(the file has no line 9)") {
		t.Errorf("line past the end: %v\n%s", f, webReviewUserText(s))
	}
}

// Spec (MOB-14): while its step still runs, or while the AI answers, the
// notes wait: busy, nothing sent.
func TestWebAIWorktreeNotesBusy(t *testing.T) {
	r, _ := webAIWorktreeRig(t)
	s := r.m.aiS()
	args := &webproto.AIWorktreeNotesArgs{ID: "old-x", Notes: []webproto.ReviewNote{{Path: "a.txt", Line: 1, Side: "new", Text: "x"}}}
	s.streaming = true
	if code := webErrCode(r.call(webproto.OpAIWorktreeNotes, args)); code != "busy" {
		t.Errorf("streaming: %s", code)
	}
	s.streaming = false
	s.conv = ai.Conversation{ID: "c1", Messages: []ai.StoredMessage{{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1"}, Steps: []ai.StoredStep{
		{ID: "left", Title: "Left", State: "running", Worktree: &ai.StoredWorktree{ID: "old-x", Branch: git.AgentBranchPrefix + "old-x", State: ai.WorktreeRunning}},
	}}}}
	s.run = &agentrun.Handle{}
	if code := webErrCode(r.call(webproto.OpAIWorktreeNotes, args)); code != "busy" || len(s.conv.Messages) != 1 {
		t.Errorf("running step: %s, %d messages", code, len(s.conv.Messages))
	}
}

// Spec (MOB-14): a quoted line is one line, cut to webReviewQuote bytes;
// control characters become spaces; a step title names the step.
func TestWebReviewPromptShape(t *testing.T) {
	long := strings.Repeat("é", webReviewQuote)
	fs := []git.AgentFile{{AgentChange: git.AgentChange{Path: "b.go"}, Before: "", After: "x\x1by\r\n" + long + "\n"}}
	notes := []webproto.ReviewNote{{Path: "b.go", Line: 1, Side: "new", Text: "one\ntwo"}, {Path: "b.go", Line: 2, Side: "new", Text: "long"}}
	got, missing := webReviewPrompt("br", "Fix\nlogin", notes, fs)
	if missing != "" {
		t.Fatalf("missing %q", missing)
	}
	for _, want := range []string{"branch br (step: Fix login):", "   > x y\n   one\n   two\n", "…\n   long\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "   > ") && len(l) > len("   > ")+webReviewQuote+len("…") {
			t.Errorf("quote too long: %d bytes", len(l))
		}
	}
	if _, missing := webReviewPrompt("br", "", []webproto.ReviewNote{{Path: "c.go", Line: 1, Side: "old", Text: "x"}}, fs); missing != "c.go" {
		t.Errorf("missing %q", missing)
	}
}
