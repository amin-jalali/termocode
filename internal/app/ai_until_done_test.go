package app

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
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
