package app

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/toast"
)

// OM-3: "work until done" in the chat — the until-done profile plans,
// does, verifies, and while verify fails a fix step runs and verify runs
// again; rounds and the verdict show on the cards, the run row and
// AGENTS.

const untilDonePlan = "```json\n" + `{"nodes": [{"id": "do", "title": "Do", "prompt": "TASK-do"}]}` + "\n```"

func sysOf(r ai.Request) string {
	if len(r.Messages) == 0 {
		return ""
	}
	return r.Messages[0].Content
}

func isVerify(r ai.Request) bool { return strings.Contains(sysOf(r), "You are the verify step") }
func isFix(r ai.Request) bool    { return strings.Contains(sysOf(r), "You are the fix step") }

// untilDoneReply answers as the planner, the planned "do" step, the fix
// step, and as verify with verdicts[n] on its n-th request (the last one
// repeats).
func untilDoneReply(verdicts ...string) func(ai.Request) ai.Chunk {
	var n atomic.Int32
	return func(r ai.Request) ai.Chunk {
		switch {
		case isPlanner(r):
			return ai.Chunk{Delta: untilDonePlan}
		case isVerify(r):
			i := int(n.Add(1)) - 1
			return ai.Chunk{Delta: verdicts[min(i, len(verdicts)-1)]}
		case isFix(r):
			return ai.Chunk{Delta: "fixed a.go:3"}
		case strings.Contains(lastUserContent(r), "### Task\nTASK-do"):
			return ai.Chunk{Delta: "did it"}
		}
		return ai.Chunk{Delta: "?"}
	}
}

func aiUntilDoneModel(t *testing.T, p ai.Provider) *Model {
	t.Helper()
	m, _ := aiRunModel(t, p, p)
	m.ai.profile = ai.ProfileUntilDone
	return m
}

// Spec: verify FAIL → a Fix card appears like a planned card, round 2
// runs fix then verify, verify passes: the cards say round 2/3, the run
// row says "2 rounds · verify ✓" and the chat file keeps the rounds.
func TestAIUntilDoneRunRounds(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("Ran go test.\nVERDICT: FAIL\na.go:3 nil map", "All green.\nVERDICT: PASS")}
	m := aiUntilDoneModel(t, p)
	driveAIChat(t, m, m.sendAIChat("fix the tests"), nil)

	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Run.Verdict != "pass" || rm.Run.Rounds != 2 || rm.Run.MaxRounds != 3 {
		t.Fatalf("run %+v", rm.Run)
	}
	if !strings.Contains(rm.Text, "All green") {
		t.Errorf("answer %q", rm.Text)
	}
	ids := []string{}
	for _, st := range rm.Steps {
		ids = append(ids, st.ID)
	}
	if strings.Join(ids, ",") != "planner,verify,do,fix" {
		t.Fatalf("steps %v", ids)
	}
	fix, verify := aiFindStep(rm, "fix"), aiFindStep(rm, "verify")
	if !fix.Planned || fix.Round != 2 || fix.State != string(agentdag.StateCompleted) || verify.Round != 2 {
		t.Errorf("fix %+v verify %+v", fix, verify)
	}
	if d := strings.Join(verify.Deps, ","); d != "do,fix" {
		t.Errorf("verify deps %q", d)
	}
	got := aiTranscriptText(m, 120)
	wantAll(t, got, "Fix · gpt-5-mini · round 2/3", "Verify · claude-sonnet-4-5 · round 2/3", "2 rounds · verify ✓", "!Not done")

	fr, ok := p.find(isFix)
	if !ok {
		t.Fatal("no fix request")
	}
	if in := lastUserContent(fr); !strings.Contains(in, "### Verify report (round 1)\nRan go test.\na.go:3 nil map") {
		t.Errorf("fix input:\n%s", in)
	}
	if !toolNames(fr)["write_file"] {
		t.Error("the fix step must be able to write files (through the review)")
	}
	vr, _ := p.find(isVerify)
	if toolNames(vr)["write_file"] || !toolNames(vr)["run_command"] {
		t.Errorf("verify tools %v", toolNames(vr))
	}
	saved, err := m.ai.store.Load(m.ai.conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r := saved.Messages[len(saved.Messages)-1].Run; r == nil || r.Rounds != 2 || r.Verdict != "pass" {
		t.Errorf("saved run %+v", r)
	}
}

// Spec: verify keeps failing → after max rounds the run ends with
// verdict fail; the row says "verify ✘ (3 rounds)", a clear line says it
// is not done, and the answer is the last verify report.
func TestAIUntilDoneGivesUp(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("VERDICT: FAIL\nb.go:9 still broken")}
	m := aiUntilDoneModel(t, p)
	driveAIChat(t, m, m.sendAIChat("fix the tests"), nil)

	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Run.Verdict != "fail" || rm.Run.Rounds != 3 {
		t.Fatalf("run %+v", rm.Run)
	}
	if !strings.Contains(rm.Text, "b.go:9 still broken") {
		t.Errorf("answer %q", rm.Text)
	}
	wantAll(t, aiTranscriptText(m, 120), "verify ✘ (3 rounds)", "Not done after 3 rounds: verify still fails", "round 3/3")
	n := 0
	for _, r := range p.reqs {
		if isFix(r) {
			n++
		}
	}
	if n != 2 {
		t.Errorf("fix ran %d times, want 2", n)
	}
}

// Spec: a verify answer without a VERDICT line is asked once more; when
// the line is still missing it counts as FAIL and the card says so.
func TestAIUntilDoneMissingVerdict(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("looks fine", "still no verdict", "VERDICT: PASS")}
	m := aiUntilDoneModel(t, p)
	driveAIChat(t, m, m.sendAIChat("fix the tests"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.Verdict != "pass" || rm.Run.Rounds != 2 {
		t.Fatalf("run %+v", rm.Run)
	}
	if !strings.Contains(rm.Run.Report, "no VERDICT line (counted as FAIL)") {
		t.Errorf("report %q", rm.Run.Report)
	}
	m.ai.conv.Folded = map[string]bool{aiCardID(rm.Run.ID, "verify"): false}
	m.ai.runGen++
	wantAll(t, aiTranscriptText(m, 140), "↻ no VERDICT line, asked once more",
		"✘ round 1/3: no VERDICT line, even after asking again: counted as FAIL", "✓ round 2/3: verify passed")
}

// Spec (OM-6 + OM-3): an until-done run interrupted by a restart in
// round 2 (while fix ran) resumes in round 2: no planner or "do"
// request, fix gets the saved report, verify passes, the run keeps its
// rounds.
func TestAIUntilDoneResumeKeepsRound(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("All green.\nVERDICT: PASS")}
	m := aiUntilDoneModel(t, p)
	c := ai.NewConversation()
	c.Messages = []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "fix the tests"},
		{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r7", Profile: ai.ProfileUntilDone, State: ai.RunStateRunning,
			Rounds: 2, MaxRounds: 3, Report: "a.go:3 nil map"}, Steps: []ai.StoredStep{
			{ID: "planner", Title: "Planner", Category: "deep", State: "completed", Text: untilDonePlan},
			{ID: "verify", Title: "Verify", Category: "deep", State: "pending", Round: 1, Text: "VERDICT: FAIL\na.go:3 nil map", Deps: []string{"do", "fix"}},
			{ID: "do", Title: "Do", Category: "quick", State: "completed", Text: "did it", Planned: true, Prompt: "TASK-do", Deps: []string{"planner"}},
			{ID: "fix", Title: "Fix", Category: "quick", State: "running", Round: 2, Planned: true},
		}},
	}
	if err := m.ai.store.Save(&c); err != nil {
		t.Fatal(err)
	}
	m.onAIHistoryPicked(c.ID)
	if m.ai.aiRetryStep(1) == 0 {
		t.Fatal("an interrupted until-done run must be resumable")
	}
	wantAll(t, aiTranscriptText(m, 120), "↻ Resume (2 of 4 left)")
	driveAIChat(t, m, m.onAIButton("run-retry:1"), nil)

	for _, r := range p.reqs {
		if isPlanner(r) || strings.Contains(lastUserContent(r), "TASK-do") && !isVerify(r) && !isFix(r) {
			t.Errorf("a finished step ran again: %q", sysOf(r)[:min(80, len(sysOf(r)))])
		}
	}
	fr, ok := p.find(isFix)
	if !ok || !strings.Contains(lastUserContent(fr), "### Verify report (round 1)\na.go:3 nil map") {
		t.Errorf("fix input:\n%s", lastUserContent(fr))
	}
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || rm.Run.Verdict != "pass" || rm.Run.Rounds != 2 {
		t.Fatalf("run %+v", rm.Run)
	}
	if st := aiFindStep(rm, "verify"); st.Round != 2 || st.State != string(agentdag.StateCompleted) {
		t.Errorf("verify %+v", st)
	}
}

// Spec: AGENTS shows the run's round in its header and on the cards of
// round 2+.
func TestAgentsPanelRounds(t *testing.T) {
	agentsUnicode(t)
	m := agentsModel(t,
		ai.StoredMessage{Role: ai.StoredUser, Text: "fix the tests"},
		ai.StoredMessage{Role: ai.StoredAssistant, Run: &ai.StoredRun{ID: "r1", Profile: ai.ProfileUntilDone, State: ai.RunStateDone,
			Rounds: 2, MaxRounds: 3, Verdict: "pass"}, Steps: []ai.StoredStep{
			{ID: "planner", Title: "Planner", State: "completed"},
			{ID: "verify", Title: "Verify", State: "completed", Round: 2, Deps: []string{"do", "fix"}},
			{ID: "do", Title: "Do", State: "completed", Planned: true, Prompt: "x", Deps: []string{"planner"}},
			{ID: "fix", Title: "Fix", State: "completed", Round: 2, Planned: true},
		}})
	got := agentsText(m, 100, 40)
	wantAll(t, got, "run r1 · until-done · round 2/3", "round 2/3")
	if strings.Count(got, "round 2/3") < 3 {
		t.Errorf("want the header and two cards with round 2/3:\n%s", got)
	}
}

// Spec: the fix step's file writes still open the review card.
func TestAIUntilDoneFixWritesNeedReview(t *testing.T) {
	write := ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": reviewNew}}}}
	var fixCalls atomic.Int32
	verdicts := untilDoneReply("VERDICT: FAIL\na.txt:2 wrong case", "VERDICT: PASS")
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk {
		if isFix(r) {
			if fixCalls.Add(1) == 1 {
				return write
			}
			return ai.Chunk{Delta: "fixed"}
		}
		return verdicts(r)
	}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	t.Cleanup(clock.Set(func() time.Time { return time.Date(2026, 10, 18, 9, 5, 0, 0, time.UTC) }))
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain:  {p: p, cfg: ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-4-5"}},
		ai.SlotQuick: {p: p, cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.ai.profile = ai.ProfileUntilDone
	reviewed := false
	driveAIReview(t, m, m.sendAIChat("fix the case"), func(m *Model) {
		if !m.reviewOpen {
			t.Fatal("review card not open")
		}
		reviewed = true
		reviewKey(t, m, "a")
		reviewKey(t, m, "a")
	})
	if !reviewed {
		t.Fatal("the fix step wrote without a review")
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewNew {
		t.Errorf("a.txt %q", got)
	}
	if rm := aiRunMsg(t, m); rm.Run.Verdict != "pass" || rm.Run.Rounds != 2 {
		t.Errorf("run %+v", rm.Run)
	}
}

// Spec: "/done <message>" runs that one message with until-done; the
// stored message has no prefix and the active profile does not change;
// the next message uses the active profile again.
func TestAIDoneKeywordRunsUntilDone(t *testing.T) {
	p := &funcAIProvider{reply: func(r ai.Request) ai.Chunk {
		if isPlanner(r) || isVerify(r) || isFix(r) || strings.Contains(lastUserContent(r), "TASK-do") {
			return untilDoneReply("VERDICT: PASS")(r)
		}
		return ai.Chunk{Delta: "plain answer"}
	}}
	m, _ := aiRunModel(t, p, p)
	m.ai.profile = ai.ProfileDefault
	driveAIChat(t, m, m.sendAIChat("/done fix the tests"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.Profile != ai.ProfileUntilDone || rm.Run.Verdict != "pass" {
		t.Fatalf("run %+v", rm.Run)
	}
	if u := m.ai.conv.Messages[0]; u.Role != ai.StoredUser || u.Text != "fix the tests" {
		t.Errorf("stored user message %+v", u)
	}
	if got := m.ai.activeProfile().Name; got != ai.ProfileDefault {
		t.Errorf("active profile %q", got)
	}
	n := len(m.ai.conv.Messages)
	driveAIChat(t, m, m.sendAIChat("and now?"), nil)
	if last := m.ai.conv.Messages[len(m.ai.conv.Messages)-1]; len(m.ai.conv.Messages) != n+2 || last.Run != nil || last.Text != "plain answer" {
		t.Errorf("the next message must use the default profile: %+v", last)
	}
	// "/done" alone is a plain message.
	driveAIChat(t, m, m.sendAIChat("/done"), nil)
	if last := m.ai.conv.Messages[len(m.ai.conv.Messages)-1]; last.Run != nil {
		t.Errorf("a bare /done started a run: %+v", last.Run)
	}
}

// Spec: AI: Work Until Done (toggle) switches to until-done (the chip
// says so, a toast names the limits) and back to the profile before.
func TestAIUntilDoneToggle(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	m.ai.profile = ai.ProfileReview
	cmd, ok := m.dispatchAIPalette("ai-until-done")
	if !ok {
		t.Fatal("ai-until-done not dispatched")
	}
	_ = cmd
	if got := m.ai.activeProfile().Name; got != ai.ProfileUntilDone {
		t.Fatalf("profile %q", got)
	}
	if chip := m.aiProfileChipLabel(); !strings.Contains(chip, "until-done") {
		t.Errorf("chip %q", chip)
	}
	if top := ansi.Strip(strings.Join(m.toast.View(160, 40), "\n")); !strings.Contains(top, "Work until done: on") ||
		!strings.Contains(top, "up to 3 rounds") || !strings.Contains(top, "$2.00") {
		t.Errorf("toast %q", top)
	}
	if loadSession().AIProfile != ai.ProfileUntilDone {
		t.Error("the choice is not stored for the workspace")
	}
	m.dispatchAIPalette("ai-until-done")
	if got := m.ai.activeProfile().Name; got != ai.ProfileReview {
		t.Errorf("toggle off: profile %q, want review", got)
	}
	if p, ok := aiActionPolicyFor("ai-until-done"); !ok || p != aiConfirm(ai.ClassSettings) {
		t.Errorf("policy %+v", p)
	}
}

// Spec (owner 2026-10-25): the Settings rows ai_until_done_rounds and
// ai_until_done_budget_usd change the defaults (saved in config.json);
// a run then stops after that many rounds.
func TestAIUntilDoneSettingsChangeDefaults(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("VERDICT: FAIL\nstill broken")}
	m := aiUntilDoneModel(t, p)
	if v, _ := aiSettingValue("setting-ai-until-done-rounds"); v != "3" {
		t.Errorf("default rounds row %q", v)
	}
	if v, _ := aiSettingValue("setting-ai-until-done-budget"); v != "2.00" {
		t.Errorf("default budget row %q", v)
	}
	m.applySetting("setting-ai-until-done-rounds", "2")
	m.applySetting("setting-ai-until-done-budget", "0.5")
	path, _ := ai.ConfigPath()
	s := ai.LoadSettings(path)
	if s.UntilDoneRounds != 2 || s.UntilDoneBudgetUSD == nil || *s.UntilDoneBudgetUSD != 0.5 {
		t.Fatalf("saved %+v", s)
	}
	if v, _ := aiSettingValue("setting-ai-until-done-budget"); v != "0.50" {
		t.Errorf("budget row %q", v)
	}
	// applySetting reloads the provider; the test's fake comes back.
	m.ai.provider, m.ai.cfg = p, ai.Config{Kind: ai.KindOllama, Model: "fake-1"}
	m.ai.slotFake = aiDemoSlots(p, p)
	m.ai.settings.PlanApproval = ai.PlanApprovalAuto // G-PLAN: the card has its own tests
	prof, _ := m.ai.profiles.Get(ai.ProfileUntilDone)
	if prof.BudgetUSD != 0.5 || prof.Loop.MaxRounds != 2 {
		t.Fatalf("profile %+v loop %+v", prof, prof.Loop)
	}
	driveAIChat(t, m, m.sendAIChat("fix the tests"), nil)
	if rm := aiRunMsg(t, m); rm.Run.Verdict != "fail" || rm.Run.Rounds != 2 || rm.Run.MaxRounds != 2 {
		t.Errorf("run %+v", rm.Run)
	}
	m.applySetting("setting-ai-until-done-budget", "0")
	if prof, _ := m.ai.profiles.Get(ai.ProfileUntilDone); prof.BudgetUSD != 0 {
		t.Errorf("0 must mean no budget: %v", prof.BudgetUSD)
	}
	if v, _ := aiSettingValue("setting-ai-until-done-budget"); v != "0 (no budget)" {
		t.Errorf("no-budget row %q", v)
	}
}

// Spec: invalid values are refused with a clear error and nothing is
// saved; a bad value written by hand falls back to the default and is
// named in the profile problems toast.
func TestAIUntilDoneSettingsRejected(t *testing.T) {
	m := aiUntilDoneModel(t, &recordingAIProvider{})
	path, _ := ai.ConfigPath()
	for _, c := range []struct{ id, value, want string }{
		{"setting-ai-until-done-rounds", "0", "ai_until_done_rounds must be 1 to 10"},
		{"setting-ai-until-done-rounds", "11", "ai_until_done_rounds must be 1 to 10"},
		{"setting-ai-until-done-rounds", "lots", "whole number from 1 to 10"},
		{"setting-ai-until-done-budget", "-1", "ai_until_done_budget_usd must be a dollar amount above 0, or 0 for no budget"},
		{"setting-ai-until-done-budget", "cheap", "ai_until_done_budget_usd must be a dollar amount"},
	} {
		m.toast = toast.New()
		m.applySetting(c.id, c.value)
		if top := ansi.Strip(strings.Join(m.toast.View(200, 40), "\n")); !strings.Contains(top, c.want) {
			t.Errorf("%s=%s: toast %q, want %q", c.id, c.value, top, c.want)
		}
		if s := ai.LoadSettings(path); s.UntilDoneRounds != 0 || s.UntilDoneBudgetUSD != nil {
			t.Errorf("%s=%s was saved: %+v", c.id, c.value, s)
		}
	}
	m.ai.settings = ai.Settings{UntilDoneRounds: 42}
	m.ai.reloadProfiles()
	if prof, _ := m.ai.profiles.Get(ai.ProfileUntilDone); prof.Loop.MaxRounds != 3 {
		t.Errorf("a bad setting must fall back to 3: %d", prof.Loop.MaxRounds)
	}
	if got := strings.Join(m.ai.profiles.Errors, "\n"); !strings.Contains(got, "ai_until_done_rounds must be 1 to 10 (it is 42); using 3") {
		t.Errorf("profile errors %q", got)
	}
}

// Spec: a profile's own loop.max_rounds and budget_usd win over the
// settings.
func TestAIUntilDoneProfileOverrideWins(t *testing.T) {
	p := &funcAIProvider{reply: untilDoneReply("VERDICT: FAIL\nstill broken")}
	m := aiUntilDoneModel(t, p)
	writeProfiles(t, `{"profiles": {"strict": {
  "steps": [{"id": "planner", "plan": true, "category": "deep", "tools": {"preset": "read-only"}}, {"id": "verify", "category": "deep"}],
  "loop": {"verify": "verify", "fix": {"category": "quick", "tools": {"preset": "all"}}, "max_rounds": 4},
  "budget_usd": 7
}}}`)
	m.ai.settings = ai.Settings{UntilDoneRounds: 2, UntilDoneBudgetUSD: func() *float64 { v := 0.25; return &v }(), PlanApproval: ai.PlanApprovalAuto}
	m.ai.reloadProfiles()
	strict, ok := m.ai.profiles.Get("strict")
	if !ok || strict.Loop.MaxRounds != 4 || strict.BudgetUSD != 7 {
		t.Fatalf("strict %+v", strict)
	}
	if ud, _ := m.ai.profiles.Get(ai.ProfileUntilDone); ud.Loop.MaxRounds != 2 || ud.BudgetUSD != 0.25 {
		t.Errorf("until-done %+v", ud)
	}
	m.ai.profile = "strict"
	driveAIChat(t, m, m.sendAIChat("fix the tests"), nil)
	if rm := aiRunMsg(t, m); rm.Run.Rounds != 4 || rm.Run.MaxRounds != 4 || rm.Run.Verdict != "fail" {
		t.Errorf("run %+v", rm.Run)
	}
}
