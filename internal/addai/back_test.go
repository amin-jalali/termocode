package addai

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// AIP-6: the card opened from a profile step can start at one AI (a
// step's Fix), and its last button says where it goes back to.
func TestStartAtAndBack(t *testing.T) {
	m := New(Options{Back: "Back to the profile"})
	m.SetSize(120, 40)

	// Two ways: the card starts on the way choice of that AI.
	a, cmd := m.StartAt(ai.KindAnthropic, "")
	if a.Step() != StepMethod || a.Kind() != ai.KindAnthropic || cmd != nil {
		t.Fatalf("anthropic: step %v kind %q", a.Step(), a.Kind())
	}
	// A way given: straight to it (a CLI that is on this machine signs in).
	m2 := New(Options{Installed: map[string]bool{ai.CLICodex: true}})
	b, cmd := m2.StartAt(ai.KindOpenAI, ai.MethodCLI)
	if b.Step() != StepSignIn || cmd == nil {
		t.Fatalf("openai sign-in: step %v", b.Step())
	}
	if msg, ok := cmd().(SignInMsg); !ok || msg.CLI != ai.CLICodex {
		t.Errorf("sign-in request %#v", msg)
	}
	// One way: the local AI is looked for at once.
	c, cmd := m.StartAt(ai.KindOllama, "")
	if c.Step() != StepLocal || cmd == nil {
		t.Fatalf("ollama: step %v", c.Step())
	}
	if _, ok := cmd().(DetectLocalMsg); !ok {
		t.Error("no detect request")
	}
	c.Finish(c.Gen(), []string{"llama3.2:3b"})
	out := text(c)
	if !strings.Contains(out, "Back to the profile") || strings.Contains(out, "  Done  ") {
		t.Errorf("done card:\n%s", out)
	}
	if msg := press(t, &c, "done"); msg != (DoneMsg{Kind: ai.KindOllama}) {
		t.Errorf("done %#v", msg)
	}
	// An unknown kind leaves the list.
	d, cmd := m.StartAt("nope", "")
	if d.Step() != StepPick || cmd != nil {
		t.Errorf("unknown: step %v", d.Step())
	}
}
