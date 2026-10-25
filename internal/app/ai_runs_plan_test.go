package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
)

// U20 AP-8: planned runs, waves, the confirm queue and the budget pause
// in the chat.

// funcAIProvider answers each request with reply(request) and records
// the requests (safe for parallel steps).
type funcAIProvider struct {
	reply func(ai.Request) ai.Chunk
	mu    sync.Mutex
	reqs  []ai.Request
}

func (*funcAIProvider) Name() string        { return "func" }
func (*funcAIProvider) Configured() bool    { return true }
func (*funcAIProvider) SupportsTools() bool { return true }
func (p *funcAIProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	req.Messages = append([]ai.Message(nil), req.Messages...)
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	r := p.reply(req)
	ch := make(chan ai.Chunk, 2)
	if r.Delta != "" {
		ch <- ai.Chunk{Delta: r.Delta}
	}
	ch <- ai.Chunk{Done: true, ToolCalls: r.ToolCalls, Usage: r.Usage}
	close(ch)
	return ch
}

func (p *funcAIProvider) find(match func(ai.Request) bool) (ai.Request, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.reqs {
		if match(r) {
			return r, true
		}
	}
	return ai.Request{}, false
}

func isPlanner(r ai.Request) bool {
	return len(r.Messages) > 0 && strings.Contains(r.Messages[0].Content, "You are the planner")
}

const appDiamondPlan = "```json\n" + `{"nodes": [
  {"id": "map", "title": "Map", "prompt": "TASK-map", "category": "quick"},
  {"id": "left", "title": "Left", "prompt": "TASK-left", "deps": ["map"]},
  {"id": "right", "title": "Right", "prompt": "TASK-right", "deps": ["map"]},
  {"id": "verify", "title": "Verify", "prompt": "TASK-verify", "category": "deep", "deps": ["left", "right"]}
]}` + "\n```"

// planReply answers as the planner (plan) or as the planned step whose
// task is in the request.
func planReply(plan string) func(ai.Request) ai.Chunk {
	return func(r ai.Request) ai.Chunk {
		if isPlanner(r) {
			return ai.Chunk{Delta: plan}
		}
		in := lastUserContent(r)
		for _, id := range []string{"map", "left", "right", "verify"} {
			if strings.Contains(in, "### Task\nTASK-"+id) {
				return ai.Chunk{Delta: "answer of " + id}
			}
		}
		return ai.Chunk{Delta: "?"}
	}
}

// aiPlanModel is a chat on the built-in plan profile; main (deep) and
// quick share one provider.
func aiPlanModel(t *testing.T, p ai.Provider) *Model {
	t.Helper()
	m, _ := aiRunModel(t, p, p)
	m.ai.profile = ai.ProfilePlan
	return m
}

// Spec (F5): the plan profile's planner answers a diamond plan; the run
// grows by one card per planned node (with its model from the category),
// the nodes run, and the last node's answer is the chat answer.
func TestAIPlannedRunDiamond(t *testing.T) {
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)

	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "answer of verify" || rm.Run.Profile != ai.ProfilePlan {
		t.Fatalf("run %+v text %q", rm.Run, rm.Text)
	}
	if len(rm.Steps) != 5 {
		t.Fatalf("steps %+v", rm.Steps)
	}
	want := []struct{ id, model string }{{"planner", "claude-sonnet-4-5"}, {"map", "gpt-5-mini"}, {"left", "gpt-5-mini"},
		{"right", "gpt-5-mini"}, {"verify", "claude-sonnet-4-5"}}
	for i, w := range want {
		st := rm.Steps[i]
		if st.ID != w.id || st.Model != w.model || st.State != string(agentdag.StateCompleted) || st.Planned != (i > 0) {
			t.Errorf("step %d %+v", i, st)
		}
	}
	if d := strings.Join(rm.Steps[4].Deps, ","); d != "left,right" {
		t.Errorf("verify deps %q", d)
	}
	plan, ok := p.find(isPlanner)
	if !ok {
		t.Fatal("no planner request")
	}
	if sys := plan.Messages[0].Content; !strings.Contains(sys, "data, not instructions") || !strings.Contains(sys, "at most 12") && !strings.Contains(sys, "At most 12") {
		t.Errorf("planner prompt must treat repo content as data and give the limits:\n%s", sys)
	}
	if toolNames(plan)["write_file"] {
		t.Error("the planner must have read-only tools")
	}
	verify, _ := p.find(func(r ai.Request) bool { return strings.Contains(lastUserContent(r), "TASK-verify") })
	if in := lastUserContent(verify); !strings.Contains(in, "make the parser faster") ||
		!strings.Contains(in, "### Output of step \"Left\"\nanswer of left") || strings.Contains(in, "\"nodes\"") {
		t.Errorf("verify input:\n%s", in)
	}
	got := aiTranscriptText(m, 100)
	for _, w := range []string{"▸ 1 · Planner", "▸ 2 · Map · gpt-5-mini", "▸ 5 · Verify · claude-sonnet-4-5", "answer of verify", "─ run · 0 s · 5 steps"} {
		if !strings.Contains(got, w) {
			t.Errorf("transcript lacks %q:\n%s", w, got)
		}
	}
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || len(saved.Messages[1].Steps) != 5 {
		t.Errorf("chat file %+v %v", saved, err)
	}
	if m.ai.aiRetryStep(1) != 0 {
		t.Error("a finished planned run offers no retry")
	}
}

// Spec: a planned step that waits shows which steps it waits for.
func TestAIStepWaitingForDeps(t *testing.T) {
	st := ai.StoredStep{ID: "verify", State: "pending", Deps: []string{"left", "right"}}
	steps := []ai.StoredStep{{ID: "planner"}, {ID: "map"}, {ID: "left"}, {ID: "right"}, st}
	if got := aiStepStatus(steps, 4); got != "waiting for steps 3, 4" {
		t.Errorf("status %q", got)
	}
	if got := aiStepStatus([]ai.StoredStep{{ID: "a"}, {ID: "b", State: "pending"}}, 1); got != "waiting for step 1" {
		t.Errorf("chain status %q", got)
	}
}

// Spec: a plan that is still bad after one repair ends in a clear error
// card; nothing else runs.
func TestAIPlannedRunBadPlan(t *testing.T) {
	p := &funcAIProvider{reply: planReply("I would rather just start coding.")}
	m := aiPlanModel(t, p)
	driveAIChat(t, m, m.sendAIChat("go"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateFailed || len(rm.Steps) != 1 {
		t.Fatalf("run %+v steps %+v", rm.Run, rm.Steps)
	}
	st := rm.Steps[0]
	if st.State != string(agentdag.StateFailed) || !strings.Contains(st.Error, "plan is not valid after one repair") {
		t.Errorf("planner card %+v", st)
	}
	if len(st.Notes) != 1 || !strings.Contains(st.Notes[0], "plan not valid") {
		t.Errorf("repair note %q", st.Notes)
	}
	if n := len(p.reqs); n != 2 {
		t.Errorf("requests %d (the plan and one repair)", n)
	}
	got := aiTranscriptText(m, 120)
	if !strings.Contains(got, "✘ the plan is not valid after one repair") || !strings.Contains(got, "run failed") {
		t.Errorf("transcript:\n%s", got)
	}
}

// Spec: the confirm card of a pipeline step names the step.
func TestAIRunConfirmNamesTheStep(t *testing.T) {
	rev := &recordingAIProvider{replies: []ai.Chunk{{Delta: "set it"}}}
	fix := &scriptedAIProvider{replies: []ai.Chunk{toolCall("settings_set", map[string]any{"key": "search_max_results", "value": 7})}}
	m, _ := aiRunModel(t, rev, fix)
	var title string
	driveAIChat(t, m, m.sendAIChat("go"), func(c aiChatConfirmMsg) string {
		title = ansi.Strip(m.confirm.Box())
		if c.step != "Fixer" {
			t.Errorf("confirm step %q", c.step)
		}
		return "deny"
	})
	if !strings.Contains(title, "Fixer wants to") {
		t.Errorf("confirm card:\n%s", title)
	}
	if m.ai.reviewGate.TryAcquire() {
		m.ai.reviewGate.Release()
	} else {
		t.Error("the gate stays held after the confirm")
	}
}

const thriftyProfiles = `{"profiles": {"thrifty": {"budget_usd": 0.01, "steps": [{"id": "a", "title": "A"}, {"id": "b", "title": "B"}, {"id": "c", "title": "C"}]}}}`

// aiBudgetModel runs the thrifty profile: three steps on the main slot,
// each $0.0135 (claude-sonnet-4-5, 4000 in / 100 out), budget $0.01.
func aiBudgetModel(t *testing.T) (*Model, *funcAIProvider) {
	t.Helper()
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk {
		return ai.Chunk{Delta: "step answer", Usage: &ai.Usage{Model: "claude-sonnet-4-5", Input: 4000, Output: 100, Requests: 1, PeakContext: 4000}}
	}}
	m, _ := aiRunModel(t, p, p)
	writeProfiles(t, thriftyProfiles)
	m.ai.loadProfiles(ai.Credentials{})
	m.ai.profile = "thrifty"
	return m, p
}

// driveUntilPaused routes chat messages until the run is paused.
func driveUntilPaused(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.sendAIChat("go")
	for i := 0; i < 200; i++ {
		msg := nextAIMsg(t, cmd)
		if _, ok := m.handleAIMsg(msg); !ok {
			t.Fatalf("unrouted %T", msg)
		}
		if rm := aiRunMsg(t, m); rm.Run.State == ai.RunStatePaused {
			return
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
	t.Fatal("the run did not pause")
}

// Spec: when the run's cost reaches budget_usd it pauses before the next
// step and asks (a card and chips); Continue runs the rest.
func TestAIRunBudgetPauseContinue(t *testing.T) {
	m, p := aiBudgetModel(t)
	driveUntilPaused(t, m)
	rm := aiRunMsg(t, m)
	if len(p.reqs) != 1 || rm.Steps[0].State != "completed" || rm.Steps[1].State != "pending" {
		t.Fatalf("paused after %d requests, steps %+v", len(p.reqs), rm.Steps)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIBudget {
		t.Fatalf("budget card not open (kind %v)", m.confirmKind)
	}
	if box := ansi.Strip(m.confirm.Box()); !strings.Contains(box, "Budget $0.01 reached") || !strings.Contains(box, "Continue") {
		t.Errorf("card:\n%s", box)
	}
	got := aiTranscriptText(m, 100)
	if !strings.Contains(got, "Budget $0.01 reached") || !strings.Contains(got, "Continue") || !strings.Contains(got, "Stop") {
		t.Errorf("transcript:\n%s", got)
	}
	cmd, _ := m.handleAIConfirm("continue")
	_ = cmd
	m.confirmOpen = false
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	rm = aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || len(p.reqs) != 3 || rm.Text != "step answer" {
		t.Errorf("after continue %+v (%d requests)", rm.Run, len(p.reqs))
	}
}

// Spec: Stop (the chip) on a paused run cancels the steps that did not
// run.
func TestAIRunBudgetPauseStop(t *testing.T) {
	m, p := aiBudgetModel(t)
	driveUntilPaused(t, m)
	m.confirmOpen = false // Esc: the card closes, the chips stay
	m.onAIConfirmClosed()
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStatePaused {
		t.Fatalf("Esc must not stop the run: %+v", rm.Run)
	}
	i := len(m.ai.conv.Messages) - 1
	_ = m.onAIButton("run-stop:" + strconv.Itoa(i))
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateCancelled || len(p.reqs) != 1 || rm.Steps[1].State != "cancelled" {
		t.Errorf("after stop %+v steps %+v (%d requests)", rm.Run, rm.Steps, len(p.reqs))
	}
}

// Spec (OM-6): a planned step card keeps its task text in the chat file
// (so the run can resume after a restart); the run snapshot never holds
// it.
func TestAIPlannedStepStoresPrompt(t *testing.T) {
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil || len(saved.Messages) != 2 || len(saved.Messages[1].Steps) != 5 {
		t.Fatalf("chat file %+v %v", saved, err)
	}
	steps := saved.Messages[1].Steps
	if steps[0].Prompt != "" {
		t.Errorf("the planner card has a prompt %q", steps[0].Prompt)
	}
	for i, id := range []string{"map", "left", "right", "verify"} {
		if got := steps[i+1].Prompt; got != "TASK-"+id {
			t.Errorf("step %s prompt %q", id, got)
		}
	}
	dir, err := aiRunsDir()
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("snapshots %v", files)
	}
	if b, _ := os.ReadFile(files[0]); strings.Contains(string(b), "TASK-") || !strings.Contains(string(b), `"verify"`) {
		t.Errorf("snapshot must hold the ids but no prompts:\n%s", b)
	}
}

// savedPlannedSteps is the step cards of an interrupted diamond plan run
// as the chat file holds them: the planner, map and left finished with
// their texts; right was running; verify waited.
func savedPlannedSteps() []ai.StoredStep {
	return []ai.StoredStep{
		{ID: "planner", Title: "Planner", Category: "deep", State: "completed", Text: appDiamondPlan},
		{ID: "map", Title: "Map", Category: "quick", State: "completed", Text: "answer of map", Planned: true, Prompt: "TASK-map", Deps: []string{"planner"}},
		{ID: "left", Title: "Left", Category: "quick", State: "completed", Text: "answer of left", Planned: true, Prompt: "TASK-left", Deps: []string{"map"}},
		{ID: "right", Title: "Right", Category: "quick", State: "running", Planned: true, Prompt: "TASK-right", Deps: []string{"map"}},
		{ID: "verify", Title: "Verify", Category: "deep", State: "pending", Planned: true, Prompt: "TASK-verify", Deps: []string{"left", "right"}},
	}
}

// savePlannedChat saves a chat whose planned run was going when
// termocode stopped and opens it (the run loads as interrupted).
func savePlannedChat(t *testing.T, m *Model) {
	t.Helper()
	c := ai.NewConversation()
	c.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "make the parser faster"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r9", Profile: ai.ProfilePlan, State: ai.RunStateRunning}, Steps: savedPlannedSteps()},
	}
	if err := m.ai.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	m.onAIHistoryPicked(c.ID)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateInterrupted {
		t.Fatalf("run %+v", rm.Run)
	}
}

// Spec (OM-6): a planned run interrupted by a restart resumes from its
// unfinished nodes: the saved plan is reused (no planner request), the
// finished nodes keep their saved answers, only right and verify run.
func TestAIPlannedRunResumeAfterRestart(t *testing.T) {
	p := &funcAIProvider{reply: planReply(appDiamondPlan)}
	m := aiPlanModel(t, p)
	savePlannedChat(t, m)
	if m.ai.aiRetryStep(1) == 0 {
		t.Fatal("an interrupted planned run with its saved plan must be resumable")
	}
	driveAIChat(t, m, m.onAIButton("run-retry:1"), nil)

	var tasks []string
	for _, r := range p.reqs {
		if isPlanner(r) {
			t.Error("the planner was asked again")
			continue
		}
		in := lastUserContent(r)
		for _, id := range []string{"map", "left", "right", "verify"} {
			if strings.Contains(in, "### Task\nTASK-"+id) {
				tasks = append(tasks, id)
			}
		}
	}
	if strings.Join(tasks, ",") != "right,verify" {
		t.Errorf("requests for %v (want right, verify)", tasks)
	}
	verify, _ := p.find(func(r ai.Request) bool { return strings.Contains(lastUserContent(r), "TASK-verify") })
	if in := lastUserContent(verify); !strings.Contains(in, "### Output of step \"Left\"\nanswer of left") ||
		!strings.Contains(in, "### Output of step \"Right\"\nanswer of right") || !strings.Contains(in, "make the parser faster") {
		t.Errorf("verify input:\n%s", in)
	}
	if len(verify.Messages) > 0 {
		if sys := verify.Messages[0].Content; !strings.Contains(sys, "step \"Verify\" of a planned agent run") || strings.Contains(sys, "of 5") {
			t.Errorf("a resumed planned step keeps the planned wording:\n%s", sys)
		}
	}
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Text != "answer of verify" {
		t.Errorf("run %+v text %q", rm.Run, rm.Text)
	}
	if n := len(m.ai.conv.Messages); n != 2 {
		t.Errorf("messages %d", n)
	}
	for _, st := range rm.Steps {
		if st.State != string(agentdag.StateCompleted) {
			t.Errorf("step %s %s", st.ID, st.State)
		}
	}
	if rm.Steps[2].Text != "answer of left" || rm.Steps[4].Prompt != "TASK-verify" || !rm.Steps[4].Planned {
		t.Errorf("steps %+v", rm.Steps)
	}
}

// Spec (OM-6): the chip of an interrupted planned run says how many of
// its steps a resume runs; a plain pipeline keeps "Retry from step N".
func TestAIResumeChipText(t *testing.T) {
	m := aiPlanModel(t, &funcAIProvider{reply: planReply(appDiamondPlan)})
	savePlannedChat(t, m)
	got := aiTranscriptText(m, 100)
	wantAll(t, got, "✘ run interrupted (termocode restarted)", "↻ Resume (2 of 5 left)", "!Retry from step")

	// When left did not finish either, three steps are left.
	steps := savedPlannedSteps()
	steps[2].State = "cancelled"
	m.ai.conv.Messages[1].Steps = steps
	m.ai.runGen++
	wantAll(t, aiTranscriptText(m, 100), "↻ Resume (3 of 5 left)")
}

// Spec (OM-6): an interrupted planned run from an old chat file (no
// saved task text) cannot resume; it keeps today's behaviour.
func TestAIPlannedRunOldChatNotResumable(t *testing.T) {
	m := aiPlanModel(t, &funcAIProvider{reply: planReply(appDiamondPlan)})
	steps := savedPlannedSteps()
	for i := range steps {
		steps[i].Prompt = ""
	}
	m.ai.conv.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "go"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r9", Profile: ai.ProfilePlan, State: ai.RunStateInterrupted}, Steps: steps},
	}
	if m.ai.aiRetryStep(1) != 0 {
		t.Error("an old planned run without saved tasks offers a resume")
	}
}

func builtinProfile(t *testing.T, name string) ai.Profile {
	t.Helper()
	for _, p := range ai.BuiltinProfiles() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no built-in profile %q", name)
	return ai.Profile{}
}

func runStates(r agentdag.Run) string {
	var b []string
	for _, n := range r.Nodes {
		b = append(b, n.ID+"="+string(n.State))
	}
	return strings.Join(b, " ")
}

// Spec (OM-6): aiRebuildRun rebuilds the profile's steps from the
// profile and the planned ones from their saved task text and deps;
// finished steps are completed with their saved texts.
func TestAIRebuildRunEdges(t *testing.T) {
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	prof := builtinProfile(t, ai.ProfilePlan)
	rm := ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r9", Profile: ai.ProfilePlan}, Steps: savedPlannedSteps()}
	run, outputs, err := aiRebuildRun(rm, prof)
	if err != nil {
		t.Fatal(err)
	}
	if got := runStates(run); got != "planner=completed map=completed left=completed right=pending verify=pending" {
		t.Errorf("states %s", got)
	}
	if !run.CreatedAt.Equal(clock.Now()) || run.ID != "r9" {
		t.Errorf("run %s created %v", run.ID, run.CreatedAt)
	}
	var edges []string
	for _, e := range run.Edges {
		edges = append(edges, e.From+">"+e.To)
	}
	if got := strings.Join(edges, " "); got != "planner>map map>left map>right left>verify right>verify" {
		t.Errorf("edges %s", got)
	}
	deps := map[string]string{}
	for _, n := range run.Nodes[1:] {
		if n.Input.Deps == nil {
			t.Errorf("%s: nil Input.Deps would pass the plan JSON", n.ID)
		}
		deps[n.ID] = strings.Join(n.Input.Deps, ",")
		if n.Prompt != "TASK-"+n.ID {
			t.Errorf("%s prompt %q", n.ID, n.Prompt)
		}
	}
	if deps["map"] != "" || deps["left"] != "map" || deps["verify"] != "left,right" {
		t.Errorf("input deps %v", deps)
	}
	if run.Nodes[1].Slot != ai.SlotQuick || run.Nodes[4].Slot != ai.SlotMain {
		t.Errorf("slots %s %s", run.Nodes[1].Slot, run.Nodes[4].Slot)
	}
	if outputs["left"] != "answer of left" || outputs["planner"] != appDiamondPlan || len(outputs) != 3 {
		t.Errorf("outputs %v", outputs)
	}

	rm.Steps[3].Prompt = ""
	if _, _, err := aiRebuildRun(rm, prof); err == nil || !strings.Contains(err.Error(), "send the message again") {
		t.Errorf("a planned step without its task: %v", err)
	}
	rm = ai.StoredMessage{Run: &ai.StoredRun{ID: "r9"}, Steps: savedPlannedSteps()}
	rm.Steps[0].ID = "boss"
	if _, _, err := aiRebuildRun(rm, prof); err == nil || !strings.Contains(err.Error(), "changed since this run") {
		t.Errorf("a changed profile: %v", err)
	}
	rm = ai.StoredMessage{Run: &ai.StoredRun{ID: "r9"}, Steps: savedPlannedSteps()}
	rm.Steps[4].Deps = []string{"verify"}
	if _, _, err := aiRebuildRun(rm, prof); err == nil || !strings.Contains(err.Error(), "cannot resume") {
		t.Errorf("a broken saved graph: %v", err)
	}
}
