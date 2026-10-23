package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// pipeTestModel is a chat with a main and a quick model and the
// project profile "house" (one step on main).
func pipeTestModel(t *testing.T) *Model {
	t.Helper()
	m, _ := aiProfileTestModel(t)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: fakeAIProvider{reply: "ok"}, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	return m
}

func pipeHint(t *testing.T, m *Model, name string, cap int) string {
	t.Helper()
	p, ok := m.ai.profiles.Get(name)
	if !ok {
		t.Fatalf("no profile %q", name)
	}
	return m.aiProfileHint(p, cap)
}

// Spec (AU-1): a profile row shows its steps in order with the model of
// each step's slot.
func TestAIProfileHintShowsPipeline(t *testing.T) {
	m := pipeTestModel(t)
	cases := []struct{ profile, want string }{
		{"default", "one step (main model: claude-sonnet-4-5)"},
		{"review", "Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)"},
		{"fast", "Answer (gpt-5-mini)"},
		{"plan", "planner decides the steps (up to 12 agents, 6 waves)"},
		{"house", "project, asks first · Reviewer (claude-sonnet-4-5)"},
	}
	for _, c := range cases {
		if got := pipeHint(t, m, c.profile, 100); !strings.Contains(got, c.want) {
			t.Errorf("%s: hint %q lacks %q", c.profile, got, c.want)
		}
	}
}

// A slot with no model says so; local-only uses the local slot's model.
func TestAIProfileHintSlotNotSet(t *testing.T) {
	m := pipeTestModel(t)
	m.ai.slotFake, m.ai.slots, m.ai.cfg = map[string]aiSlot{ai.SlotMain: m.ai.slotFake[ai.SlotMain]},
		ai.NewSlotPool(map[string]ai.Config{ai.SlotLocal: {Kind: ai.KindOllama, Model: "qwen2.5-coder:7b"}}, nil), ai.Config{}
	if got := pipeHint(t, m, "fast", 100); !strings.Contains(got, "Answer (quick: not set)") {
		t.Errorf("fast: %q", got)
	}
	if got := pipeHint(t, m, "review", 100); !strings.Contains(got, "Fixer (quick: not set)") {
		t.Errorf("review: %q", got)
	}
	if got := pipeHint(t, m, "local-only", 100); !strings.Contains(got, "Answer (qwen2.5-coder:7b)") {
		t.Errorf("local-only should know its model: %q", got)
	}
}

// A narrow row drops detail step by step and never goes over its width;
// the trust state of a project profile stays.
func TestAIProfileHintFitsWidth(t *testing.T) {
	m := pipeTestModel(t)
	for _, c := range []struct {
		cap  int
		want string
	}{
		{49, "Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)"},
		{44, "Reviewer (sonnet-4-5) → Fixer (gpt-5-mini)"},
		{26, "sonnet-4-5 → gpt-5-mini"},
		{12, "2 steps"},
	} {
		got := pipeHint(t, m, "review", c.cap)
		if got != c.want {
			t.Errorf("cap %d: %q, want %q", c.cap, got, c.want)
		}
		if ansi.StringWidth(got) > c.cap {
			t.Errorf("cap %d: %q is %d cells", c.cap, got, ansi.StringWidth(got))
		}
	}
	if got := pipeHint(t, m, "house", 30); !strings.HasPrefix(got, "project, asks first") {
		t.Errorf("trust state lost: %q", got)
	}
}

// The picker itself shows the pipeline at 150 and 80 columns.
func TestAIProfilePickerShowsPipeline(t *testing.T) {
	m := pipeTestModel(t)
	for _, c := range []struct {
		w, h int
		want string
	}{
		{150, 42, "Reviewer (claude-sonnet-4-5) → Fixer (gpt-5-mini)"},
		{80, 24, "sonnet-4-5 → gpt-5-mini"},
	} {
		m.w, m.h = c.w, c.h
		m.openAIProfilePicker()
		view := ansi.Strip(m.picker.View())
		if !strings.Contains(view, c.want) {
			t.Errorf("%dx%d picker lacks %q:\n%s", c.w, c.h, c.want, view)
		}
	}
}

func showProfileText(t *testing.T, m *Model, name string) string {
	t.Helper()
	m.ai.profile = name
	m.w, m.h = 150, 42
	m.dispatchAIPalette("ai-profile-show")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileShow {
		t.Fatalf("no Show Profile card for %q", name)
	}
	return ansi.Strip(m.confirm.View())
}

// Spec (AU-2): AI: Show Profile lists numbered steps with the agent,
// slot → model, the tools when limited, and the first prompt line; and
// where the profile comes from.
func TestAIShowProfileBuiltin(t *testing.T) {
	m := pipeTestModel(t)
	view := showProfileText(t, m, "review")
	for _, want := range []string{
		"Profile review", "Review then fix", "From: built in",
		"2 steps, one after the other",
		"1 · Reviewer · main → claude-sonnet-4-5", "tools: read-only",
		"“You review code. List concrete problems with file:line. Do not edit fil",
		"2 · Fixer · quick → gpt-5-mini", "“Fix the problems the reviewer listed. Keep the changes small.”",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("card lacks %q:\n%s", want, view)
		}
	}
	// The Fixer may use every tool: no tools line after it.
	if i := strings.Index(view, "2 · Fixer"); strings.Contains(view[i:], "tools:") {
		t.Errorf("a step without limits shows a tools line:\n%s", view)
	}
}

func TestAIShowProfileProjectAndPlan(t *testing.T) {
	m := pipeTestModel(t)
	view := showProfileText(t, m, "house")
	for _, want := range []string{"From: this project's .termocode/ai-profiles.json (asks before it is used)",
		"1 · Reviewer · main → claude-sonnet-4-5", "“Follow the house style.”"} {
		if !strings.Contains(view, want) {
			t.Errorf("project card lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Second line") {
		t.Errorf("only the first prompt line belongs on the card:\n%s", view)
	}
	view = showProfileText(t, m, "plan")
	for _, want := range []string{"The planner decides the other steps: up to 12 agents in up to 6 waves",
		"1 · Planner · main → claude-sonnet-4-5 · plans the steps",
		"quick steps: quick → gpt-5-mini", "deep steps: main → claude-sonnet-4-5"} {
		if !strings.Contains(view, want) {
			t.Errorf("plan card lacks %q:\n%s", want, view)
		}
	}
}

// A slot with no model says "not set"; the card is read-only (Close).
func TestAIShowProfileNotSetAndClose(t *testing.T) {
	m := pipeTestModel(t)
	m.ai.slotFake, m.ai.slots, m.ai.cfg = map[string]aiSlot{ai.SlotMain: m.ai.slotFake[ai.SlotMain]}, nil, ai.Config{}
	view := showProfileText(t, m, "review")
	if !strings.Contains(view, "2 · Fixer · quick → not set") {
		t.Errorf("no 'not set':\n%s", view)
	}
	if c, ok := m.handleAIConfirm("close"); !ok || c != nil {
		t.Errorf("Close: cmd %v ok %v", c, ok)
	}
}

// AI: Show Profile is a screen for the user: never for the AI.
func TestAIShowProfilePaletteAndPolicy(t *testing.T) {
	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-profile-show" {
			found = e.title == "AI: Show Profile"
		}
	}
	if !found {
		t.Fatal("no AI: Show Profile palette entry")
	}
	if pol, ok := aiActionPolicyFor("ai-profile-show"); !ok || pol.Gate != aiGateNever || pol.Reason == "" {
		t.Fatalf("policy %+v ok=%v, want never with a reason", pol, ok)
	}
}
