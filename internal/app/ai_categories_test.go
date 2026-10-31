package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
)

func catModel(t *testing.T, steps ...ai.ProfileStep) (*Model, *recordingAIProvider, *recordingAIProvider) {
	t.Helper()
	main := &recordingAIProvider{replies: []ai.Chunk{{Delta: "advice one"}, {Delta: "advice two"}}}
	quick := &recordingAIProvider{replies: []ai.Chunk{{Delta: "quick one"}, {Delta: "quick two"}}}
	m, _ := aiRunModel(t, main, quick)
	m.ai.profFake = []ai.Profile{{Name: "cats", Source: ai.ProfileSourceUser, Steps: steps}}
	m.ai.reloadProfiles()
	m.ai.profile = "cats"
	return m, main, quick
}

// OMX-16: an architect step runs on the deep slot with read-only tools
// (its own tools were all) and the architect's prompt.
func TestAICategoryArchitectIsReadOnly(t *testing.T) {
	m, main, _ := catModel(t, ai.ProfileStep{ID: "a", Title: "Design", Category: "architect"}, ai.ProfileStep{ID: "b", Title: "Sum up", Category: "writing"})
	driveAIChat(t, m, m.sendAIChat("how should the cache work?"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Steps[0].Slot != ai.SlotMain || rm.Steps[1].Slot != ai.SlotQuick {
		t.Fatalf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	req := main.reqs[0]
	names := toolNames(req)
	if !names["read_file"] || names["write_file"] || names["run_command"] {
		t.Fatalf("architect tools %v", names)
	}
	if sys := req.Messages[0].Content; !strings.Contains(sys, "You are the architect") {
		t.Fatalf("architect prompt missing:\n%s", sys)
	}
}

// OMX-16: ai_categories moves a category to a slot and sets its
// temperature; a new category works like the one it is like.
func TestAICategorySettings(t *testing.T) {
	m, _, quick := catModel(t, ai.ProfileStep{ID: "a", Title: "Think", Category: "ultrabrain"}, ai.ProfileStep{ID: "b", Title: "Docs", Category: "docs-de"})
	temp := 0.3
	m.ai.settings.Categories = map[string]ai.CategoryConfig{
		"ultrabrain": {Slot: ai.SlotQuick, Temperature: &temp},
		"docs-de":    {Like: "quick", PromptAppend: "Write German."},
	}
	driveAIChat(t, m, m.sendAIChat("explain the parser"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Steps[0].Slot != ai.SlotQuick || rm.Steps[1].Slot != ai.SlotQuick {
		t.Fatalf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	if len(quick.reqs) != 2 {
		t.Fatalf("quick requests %d", len(quick.reqs))
	}
	if tp := quick.reqs[0].Temperature; tp == nil || *tp != 0.3 {
		t.Fatalf("temperature %v", tp)
	}
	if quick.reqs[1].Temperature != nil {
		t.Fatal("docs-de has no temperature")
	}
	if !strings.Contains(quick.reqs[1].Messages[0].Content, "Write German.") {
		t.Fatal("prompt_append missing")
	}
}

// OMX-16: a step with a category that does not exist does not start.
func TestAICategoryUnknown(t *testing.T) {
	m, _, _ := catModel(t, ai.ProfileStep{ID: "a", Title: "Odd", Category: "nope"})
	m.sendAIChat("go")
	for _, msg := range m.ai.conv.Messages {
		if msg.Run != nil {
			t.Fatalf("a run started: %+v", msg.Run)
		}
	}
}

// OMX-16: the planner hears the extra categories; a planned visual
// node runs on the visual category's slot.
func TestAICategoryPlannerList(t *testing.T) {
	plan := "```json\n" + `{"nodes": [{"id": "ui", "title": "UI", "prompt": "fix the colours", "category": "visual"}]}` + "\n```"
	p := &funcAIProvider{reply: func(req ai.Request) ai.Chunk {
		if isPlanner(req) {
			return ai.Chunk{Delta: plan}
		}
		return ai.Chunk{Delta: "colours fixed"}
	}}
	m := aiPlanModel(t, p)
	m.ai.settings.Categories = map[string]ai.CategoryConfig{"visual": {Slot: ai.SlotQuick}}
	driveAIChat(t, m, m.sendAIChat("make it pretty"), nil)
	req, _ := p.find(isPlanner)
	sys := req.Messages[0].Content
	if !strings.Contains(sys, "- visual: user interface work") || !strings.Contains(sys, "- architect:") {
		t.Fatalf("planner prompt lacks the categories:\n%s", sys)
	}
	rm := aiRunMsg(t, m)
	st := aiFindStep(rm, "ui")
	if st == nil || st.Category != "visual" || st.Slot != ai.SlotQuick || agentdag.State(st.State) != agentdag.StateCompleted {
		t.Fatalf("ui step %+v", st)
	}
}

// OMX-16: a quick step that answers ESCALATE runs once more on deep;
// its card says so and names the deep model; with ai_step_escalate off
// the step is told nothing and its answer stands.
func TestAICategoryEscalate(t *testing.T) {
	two := []ai.ProfileStep{{ID: "a", Title: "Fix", Category: "quick"}, {ID: "b", Title: "Sum up", Category: "quick"}}
	m, main, quick := catModel(t, two...)
	quick.replies = []ai.Chunk{{Delta: "ESCALATE: the race needs a stronger model"}, {Delta: "summary"}}
	main.replies = []ai.Chunk{{Delta: "fixed the race"}}
	driveAIChat(t, m, m.sendAIChat("fix the race"), nil)
	rm := aiRunMsg(t, m)
	st := rm.Steps[0]
	if rm.Run.State != ai.RunStateDone || st.Text != "fixed the race" || st.Slot != ai.SlotMain || st.Model != "claude-sonnet-4-5" {
		t.Fatalf("run %+v step %+v", rm.Run, st)
	}
	if !strings.Contains(strings.Join(st.Notes, "\n"), "⇧ the race needs a stronger model") {
		t.Fatalf("notes %q", st.Notes)
	}
	if !strings.Contains(quick.reqs[0].Messages[0].Content, "ESCALATE: <why>") {
		t.Fatal("the quick step was not told about ESCALATE")
	}
	if strings.Contains(main.reqs[0].Messages[0].Content, "ESCALATE: <why>") {
		t.Fatal("the deep step was told about ESCALATE")
	}

	m2, _, quick2 := catModel(t, two...)
	off := false
	m2.ai.settings.StepEscalate = &off
	quick2.replies = []ai.Chunk{{Delta: "ESCALATE: hard"}, {Delta: "summary"}}
	driveAIChat(t, m2, m2.sendAIChat("fix"), nil)
	if rm := aiRunMsg(t, m2); rm.Steps[0].Text != "ESCALATE: hard" || strings.Contains(quick2.reqs[0].Messages[0].Content, "ESCALATE: <why>") {
		t.Fatalf("off: step %+v", rm.Steps[0])
	}
}
