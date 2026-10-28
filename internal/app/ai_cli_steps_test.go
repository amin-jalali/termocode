package app

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/clock"
)

// Group CS: profile steps that run Claude Code / Codex on the user's
// subscription. The tests never start a real CLI: aiCLIRun is faked.

type fakeCLI struct {
	mu      sync.Mutex
	replies map[string][]fakeCLIReply // step id → replies in order
	specs   map[string][]clistep.Spec
	prompts map[string][]string
}

type fakeCLIReply struct {
	text string
	err  error
}

func newFakeCLI(t *testing.T, replies map[string][]fakeCLIReply, diff string) *fakeCLI {
	t.Helper()
	f := &fakeCLI{replies: replies, specs: map[string][]clistep.Spec{}, prompts: map[string][]string{}}
	old, oldDiff, oldLook := aiCLIRun, aiCLIDiff, agentLookPath
	t.Cleanup(func() { aiCLIRun, aiCLIDiff, agentLookPath = old, oldDiff, oldLook })
	aiCLIRun = f.run
	aiCLIDiff = func(context.Context, string, int) string { return diff }
	agentLookPath = func(bin string) (string, error) { return "/fake/" + bin, nil }
	return f
}

// run answers by the step id written at the top of the prompt by the
// app ("You are step … ("<title>")" is not stable, so the test steps
// carry their id in their system text).
func (f *fakeCLI) run(_ context.Context, s clistep.Spec, prompt string, emit func(ai.Event)) (clistep.Result, error) {
	id := ""
	for k := range f.replies {
		if strings.Contains(prompt, "[step:"+k+"]") {
			id = k
		}
	}
	f.mu.Lock()
	n := len(f.prompts[id])
	f.prompts[id] = append(f.prompts[id], prompt)
	f.specs[id] = append(f.specs[id], s)
	rs := f.replies[id]
	r := fakeCLIReply{text: "ok"}
	if len(rs) > 0 {
		r = rs[min(n, len(rs)-1)]
	}
	f.mu.Unlock()
	emit(ai.Event{Kind: ai.EventTurnStart})
	emit(ai.Event{Kind: ai.EventToolStart, Tool: "Read", Label: "Read", Detail: "Read a.go"})
	emit(ai.Event{Kind: ai.EventToolResult, Tool: "Read", Label: "Read", Detail: "3 lines"})
	emit(ai.Event{Kind: ai.EventText, Text: r.text})
	return clistep.Result{Text: r.text, Usage: &ai.Usage{Model: "cli-model", Input: 1200, Output: 300, Requests: 1}}, r.err
}

func (f *fakeCLI) calls(id string) ([]string, []clistep.Spec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prompts[id]...), append([]clistep.Spec(nil), f.specs[id]...)
}

// cliProfile: Claude Code works, Codex reviews, Claude Code fixes.
func cliProfile() ai.Profile {
	return ai.Profile{Name: "sub", Title: "Claude works, Codex reviews", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{
			{ID: "work", Title: "Work", CLI: ai.CLIClaude, Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}, System: "[step:work]"},
			{ID: "review", Title: "Review", CLI: ai.CLICodex, System: "[step:review]"},
		},
		Loop: &ai.ProfileLoop{Verify: "review", MaxRounds: 2,
			Fix: ai.ProfileStep{ID: "fix", Title: "Fix", CLI: ai.CLIClaude, Tools: agentdag.ToolRule{Preset: agentdag.PresetAll}, System: "[step:fix]"}}}
}

func cliRunModel(t *testing.T) (*Model, string) {
	t.Helper()
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 27, 13, 0, 0, 0, time.UTC) }))
	m, dir := aiIDETestModel(t, &recordingAIProvider{})
	m.ai.profFake = []ai.Profile{cliProfile()}
	m.ai.reloadProfiles()
	m.ai.profile = "sub"
	return m, dir
}

// CS-1/2/3: the steps run on the CLIs (never a model slot); the worker
// may write, the reviewer reads only; the hand-off carries the task, the
// earlier step's answer and the git diff; the review's VERDICT drives
// the loop; the cards show the CLI, its tool calls and its tokens, and
// the cost is the subscription (no dollars, not in the usage money).
func TestAICLIStepsRun(t *testing.T) {
	f := newFakeCLI(t, map[string][]fakeCLIReply{
		"work":   {{text: "changed a.go"}},
		"review": {{text: "a.go:3 still nil\nVERDICT: FAIL"}, {text: "Looks right.\nVERDICT: PASS"}},
		"fix":    {{text: "made the map"}},
	}, "M a.go\n```diff\n-var m map[string]int\n+var m = map[string]int{}\n```")
	m, dir := cliRunModel(t)
	m.output = newOutputStore()
	driveAIChat(t, m, m.sendAIChat("go fix the nil map"), nil)

	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Run.Verdict != agentrun.VerdictPass || rm.Run.Rounds != 2 {
		t.Fatalf("run %+v", rm.Run)
	}
	wp, ws := f.calls("work")
	rp, rs := f.calls("review")
	fp, fs := f.calls("fix")
	if len(wp) != 1 || len(rp) != 2 || len(fp) != 1 {
		t.Fatalf("calls work %d review %d fix %d", len(wp), len(rp), len(fp))
	}
	if !ws[0].Write || ws[0].CLI != ai.CLIClaude || ws[0].Dir != dir || rs[0].Write || rs[0].CLI != ai.CLICodex || !fs[0].Write {
		t.Errorf("specs work %+v review %+v fix %+v", ws[0], rs[0], fs[0])
	}
	if !strings.Contains(wp[0], "go fix the nil map") || strings.Contains(wp[0], "### Changes so far") {
		t.Errorf("work prompt:\n%s", wp[0])
	}
	for _, want := range []string{"go fix the nil map", "### Output of step \"Work\"\nchanged a.go", "### Changes so far (git diff)\nM a.go", "VERDICT: PASS"} {
		if !strings.Contains(rp[0], want) {
			t.Errorf("review prompt misses %q:\n%s", want, rp[0])
		}
	}
	if !strings.Contains(fp[0], "### Verify report (round 1)\na.go:3 still nil") || !strings.Contains(fp[0], "### Changes so far (git diff)") {
		t.Errorf("fix prompt:\n%s", fp[0])
	}
	work := aiFindStep(rm, "work")
	if work.CLI != ai.CLIClaude || work.Model != "Claude Code" || work.Usage == nil || work.Usage.Input != 1200 || work.Cost != nil {
		t.Errorf("work card %+v", work)
	}
	if len(work.Tools) != 1 || work.Tools[0] != "⚙ Read a.go → 3 lines" {
		t.Errorf("work tools %q", work.Tools)
	}
	if rev := aiFindStep(rm, "review"); rev.CLI != ai.CLICodex || rev.Model != "Codex" {
		t.Errorf("review card %+v", rev)
	}
	for _, e := range m.ai.usageLog {
		if !e.Sub || e.Cost != nil {
			t.Errorf("usage entry %+v: a CLI step is the subscription", e)
		}
	}
	it := m.ai.runsItemOf(&m.ai.conv, 1, true, clock.Now())
	if !it.sub || !it.costOK || it.cost != 0 || it.tokens == 0 {
		t.Errorf("runs item sub %v costOK %v cost %v tokens %d", it.sub, it.costOK, it.cost, it.tokens)
	}
	if got := runsSummary(it); !strings.Contains(got, "subscription") || strings.Contains(got, "$") {
		t.Errorf("summary %q", got)
	}
}

// CS-5 (preflight): a missing CLI stops the run before anything starts,
// with a toast that says what to install.
func TestAICLIStepMissingCLI(t *testing.T) {
	newFakeCLI(t, nil, "")
	agentLookPath = func(bin string) (string, error) {
		if bin == "codex" {
			return "", exec.ErrNotFound
		}
		return "/fake/" + bin, nil
	}
	m, _ := cliRunModel(t)
	_ = m.sendAIChat("go")
	if len(m.ai.conv.Messages) != 0 || m.ai.streaming {
		t.Fatalf("something ran: %+v", m.ai.conv.Messages)
	}
	if top := ansi.Strip(strings.Join(m.toast.View(140, 40), "\n")); !strings.Contains(top, "Codex is not installed") {
		t.Errorf("toast %q", top)
	}
}

// drainUntil routes run messages until ok (or the run ends).
func drainUntil(t *testing.T, m *Model, cmd tea.Cmd, ok func() bool) {
	t.Helper()
	for i := 0; cmd != nil && i < 200 && !ok(); i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			return
		}
		m.handleAIMsg(msg)
		if _, done := msg.(aiChatDoneMsg); done {
			return
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
}

// CS-4: the CLI's usage limit pauses the run at that step with "resumes
// at X"; at X (or on Resume) the same step runs again and the run goes
// on.
func TestAICLIStepLimitPausesAndResumes(t *testing.T) {
	reset := time.Date(2026, 10, 27, 16, 0, 0, 0, time.UTC)
	f := newFakeCLI(t, map[string][]fakeCLIReply{
		"work":   {{err: &ai.LimitError{CLI: ai.CLIClaude, Msg: "You've hit your limit · resets 4pm (UTC)", ResetAt: reset}}, {text: "changed a.go"}},
		"review": {{text: "VERDICT: PASS"}},
	}, "")
	m, _ := cliRunModel(t)
	cmd := m.sendAIChat("go")
	rm := aiRunMsg(t, m)
	drainUntil(t, m, cmd, func() bool { return rm.Run.State == ai.RunStatePaused })
	if rm.Run.State != ai.RunStatePaused || rm.Run.ResumeAtMs != reset.UnixMilli() || !strings.Contains(rm.Run.Limit, "hit your limit") || rm.Run.Budget != 0 {
		t.Fatalf("run %+v", rm.Run)
	}
	if m.confirmOpen {
		t.Error("a limit pause opens no budget card")
	}
	work := aiFindStep(rm, "work")
	if work.State != string(agentdag.StatePending) || len(work.Notes) == 0 || !strings.Contains(work.Notes[len(work.Notes)-1], "usage limit") {
		t.Errorf("work card %+v", work)
	}
	it := m.ai.runsItemOf(&m.ai.conv, 1, true, clock.Now())
	if it.kind != runsPaused || !strings.Contains(it.why, "usage limit") || !strings.Contains(it.why, "resumes at 16:00") {
		t.Errorf("runs item kind %v why %q", it.kind, it.why)
	}
	if d := aiLimitResumeDelay(reset, clock.Now()); d != 3*time.Hour+aiLimitResumeMargin {
		t.Errorf("auto resume in %v", d)
	}
	// A stale tick (another run) does nothing; the right one resumes.
	m.handleAIMsg(aiLimitResumeMsg{gen: m.ai.chatGen + 7, run: rm.Run.ID, at: rm.Run.ResumeAtMs})
	m.handleAIMsg(aiLimitResumeMsg{gen: m.ai.chatGen, run: rm.Run.ID, at: rm.Run.ResumeAtMs - 1}) // an earlier pause's tick
	if rm.Run.State != ai.RunStatePaused {
		t.Fatal("a stale resume tick went on")
	}
	m.handleAIMsg(aiLimitResumeMsg{gen: m.ai.chatGen, run: rm.Run.ID, at: rm.Run.ResumeAtMs})
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
	if rm := aiRunMsg(t, m); rm.Run.State != ai.RunStateDone || rm.Run.Limit != "" || rm.Run.ResumeAtMs != 0 {
		t.Fatalf("after resume %+v", rm.Run)
	}
	if wp, _ := f.calls("work"); len(wp) != 2 {
		t.Errorf("work ran %d times", len(wp))
	}
}

// A limit without a reset time pauses too; Resume goes on by hand.
func TestAICLIStepLimitNoTime(t *testing.T) {
	newFakeCLI(t, map[string][]fakeCLIReply{"work": {{err: &ai.LimitError{CLI: ai.CLIClaude, Msg: "usage limit reached"}}}}, "")
	m, _ := cliRunModel(t)
	cmd := m.sendAIChat("go")
	rm := aiRunMsg(t, m)
	drainUntil(t, m, cmd, func() bool { return rm.Run.State == ai.RunStatePaused })
	it := m.ai.runsItemOf(&m.ai.conv, 1, true, clock.Now())
	if !strings.Contains(it.why, "usage limit") || strings.Contains(it.why, "resumes at") {
		t.Errorf("why %q", it.why)
	}
	m.aiCancelAll()
	driveAIChat(t, m, waitAIChat(m.ai.chatCh), nil)
}

// CS-3/5: every place that names a step's model names the CLI and the
// subscription for a CLI step, and its cost is "subscription".
func TestAICLIStepLabels(t *testing.T) {
	m, _ := cliRunModel(t)
	steps := m.ai.aiProfileSteps(cliProfile())
	if steps[0].slot != "Claude Code" || steps[0].model != "subscription" || steps[1].slot != "Codex" {
		t.Errorf("pipeline steps %+v", steps)
	}
	if got := m.ai.profileModelLabel(cliProfile()); got != "Codex" {
		t.Errorf("model label %q", got)
	}
	st := ai.StoredStep{CLI: ai.CLIClaude, Model: "Claude Code"}
	if aiStepCost(st) != "subscription" || runsStepOn(st) != "Claude Code · subscription" {
		t.Errorf("cost %q on %q", aiStepCost(st), runsStepOn(st))
	}
	usd := 0.25
	if aiStepCost(ai.StoredStep{Cost: &usd}) != aiDollars(usd) || runsStepOn(ai.StoredStep{Model: "gpt-5"}) != "main · gpt-5" {
		t.Error("a model step keeps its dollars and slot")
	}
	if got := runsToolText(cliProfile(), "review"); got != "read-only" {
		t.Errorf("a CLI step without a preset reads only, Runs says %q", got)
	}
	if got := runsToolText(cliProfile(), "work"); got != "edit files" {
		t.Errorf("work: %q", got)
	}
	if _, _, word := (runsItem{kind: runsRunning, anyTok: true, costOK: true, sub: true}).look(runsColours{}); word != "" {
		t.Errorf("the narrow list leaves the task the room, word %q", word)
	}
	it := runsItem{anyTok: true, costOK: true, sub: true, cost: 0.5}
	if got := runsCostText(it); got != aiDollars(0.5)+" + subscription" {
		t.Errorf("mixed cost %q", got)
	}
}
