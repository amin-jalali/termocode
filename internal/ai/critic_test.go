package ai

import (
	"strings"
	"testing"
)

// CMP-53: a second model (the critic) reviews a lane's changes before
// the user merges them.

func TestUseCriticDefaultOn(t *testing.T) {
	if !(Settings{}).UseCritic() {
		t.Error("the critic is off by default")
	}
	if (Settings{Critic: CriticOff}).UseCritic() {
		t.Error(`"off" does not turn the critic off`)
	}
}

// The critic runs on the other model than the step: main ↔ quick; a
// configured slot wins.
func TestCriticSlot(t *testing.T) {
	for _, c := range []struct{ step, set, want string }{
		{"", "", SlotQuick},
		{SlotMain, "", SlotQuick},
		{SlotQuick, "", SlotMain},
		{"gpt", "", SlotMain},
		{SlotMain, "review", "review"},
	} {
		if got := CriticSlot(c.step, c.set); got != c.want {
			t.Errorf("CriticSlot(%q, %q) = %q, want %q", c.step, c.set, got, c.want)
		}
	}
}

func TestParseCritic(t *testing.T) {
	for _, c := range []struct{ text, state, notes string }{
		{"VERDICT: OK\nLooks fine.", CriticOK, "Looks fine."},
		{"  verdict: ok", CriticOK, ""},
		{"Verdict: ISSUES\n- a.go:3 nil map write\n- b.go:9 no test", CriticIssues, "- a.go:3 nil map write\n- b.go:9 no test"},
		{"\n\nVERDICT: issues — a.go:1 typo", CriticIssues, "a.go:1 typo"},
		{"I think it is mostly fine.", CriticIssues, "The critic gave no clear verdict:\nI think it is mostly fine."},
		{"   ", CriticFailed, "The critic gave no answer"},
	} {
		st, notes := ParseCritic(c.text)
		if st != c.state || notes != c.notes {
			t.Errorf("ParseCritic(%q) = %q, %q; want %q, %q", c.text, st, notes, c.state, c.notes)
		}
	}
	long := "VERDICT: ISSUES\n" + strings.Repeat("x", CriticMaxNotes+100)
	if _, notes := ParseCritic(long); len(notes) > CriticMaxNotes+len("…") {
		t.Errorf("notes not cut: %d bytes", len(notes))
	}
}

// The prompt holds the task and the diff (registered secrets hidden) and
// asks for the verdict line; the model edits nothing.
func TestCriticPrompt(t *testing.T) {
	RegisterSecret("critic-secret-4711")
	msgs := CriticPrompt("Speed up the lexer", "+key = critic-secret-4711\n", true)
	if len(msgs) != 2 || msgs[0].Role != RoleSystem || msgs[1].Role != RoleUser {
		t.Fatalf("messages %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "VERDICT: OK") || !strings.Contains(msgs[0].Content, "VERDICT: ISSUES") {
		t.Errorf("system prompt has no verdict format: %q", msgs[0].Content)
	}
	in := msgs[1].Content
	if !strings.Contains(in, "Speed up the lexer") || !strings.Contains(in, "```diff") || !strings.Contains(in, "[cut here") {
		t.Errorf("user prompt %q", in)
	}
	if strings.Contains(in, "critic-secret-4711") {
		t.Error("a registered secret reached the critic")
	}
}

func TestCriticDone(t *testing.T) {
	var c *StoredCritic
	if c.Done() {
		t.Error("nil critic is done")
	}
	if (&StoredCritic{State: CriticRunning}).Done() || !(&StoredCritic{State: CriticIssues}).Done() {
		t.Error("Done wrong")
	}
}
