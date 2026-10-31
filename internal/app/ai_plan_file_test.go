package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentrun"
	"github.com/amin-jalali/termocode/internal/ai"
)

// OMX-14: with ai_plan_review_rounds a reviewer reads the plan (a reject
// goes back to the planner); the run writes .termocode/plans/<slug>.md
// with the steps, the review rounds and how it ended.
func TestAIPlanReviewAndPlanFile(t *testing.T) {
	reviews := 0
	p := &funcAIProvider{reply: func(req ai.Request) ai.Chunk {
		switch {
		case strings.Contains(req.Messages[0].Content, "You review a plan"):
			reviews++
			if reviews == 1 {
				return ai.Chunk{Delta: "REJECT: no final check\nadd a verify step"}
			}
			return ai.Chunk{Delta: "APPROVE"}
		case isPlanner(req):
			return planReply(appDiamondPlan)(req)
		}
		return ai.Chunk{Delta: "answer"}
	}}
	m := aiPlanModel(t, p)
	m.ai.settings.PlanReviewRounds = 2
	on := true
	m.ai.settings.PlanFile = &on
	driveAIChat(t, m, m.sendAIChat("make the parser faster"), nil)
	rm := aiRunMsg(t, m)
	if rm.Run.State != ai.RunStateDone || reviews != 2 {
		t.Fatalf("run %+v reviews %d", rm.Run, reviews)
	}
	notes := strings.Join(rm.Steps[0].Notes, "\n")
	wantAll(t, notes, "✘ plan review 1: no final check", "✓ plan review 2: approved", "plan saved: .termocode/plans/make-the-parser-faster-")
	matches, _ := filepath.Glob(filepath.Join(aiWorkspaceRoot(), ".termocode", "plans", "make-the-parser-faster-*.md"))
	if len(matches) != 1 {
		t.Fatalf("plan files %v", matches)
	}
	b, _ := os.ReadFile(matches[0])
	wantAll(t, string(b), "# Plan: make the parser faster", "## Steps", "**Map**", "## Review", "round 1: reject — no final check",
		"round 2: approve", "## Result", "done")
}

// OMX-14: a verify step's evidence goes into the plan file.
func TestAIPlanFileEvidence(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	on := true
	m.ai.settings.PlanFile = &on
	rm := &ai.StoredMessage{Run: &ai.StoredRun{ID: "r9", Profile: "plan", State: ai.RunStateDone, Verdict: "pass"},
		Steps: []ai.StoredStep{{ID: "v", Title: "Verify", Planned: true}}}
	d := m.ai.aiPlanDocOf(rm, "fix it")
	m.aiPlanFileEvent(rm, agentrun.Event{RunID: "r9", Kind: agentrun.NodeDone, NodeID: "v", Verdict: agentrun.VerdictPass,
		Output: "ok\nEVIDENCE:\n- go test ./... → ok\n\nVERDICT: PASS"})
	m.ai.aiPlanFileEnd(rm)
	b, err := os.ReadFile(d.path)
	if err != nil {
		t.Fatal(err)
	}
	wantAll(t, string(b), "## Verify evidence", "- go test ./... → ok", "done · verify pass")
}

// OMX-14: ai_plan_file false writes no file, and by default (unset) a
// run writes none either: no untracked file in the user's repository.
func TestAIPlanFileOff(t *testing.T) {
	m, _ := aiRunModel(t, &recordingAIProvider{}, &recordingAIProvider{})
	if m.ai.aiPlanDocOf(&ai.StoredMessage{Run: &ai.StoredRun{ID: "r0"}}, "x") != nil {
		t.Fatal("a plan doc by default")
	}
	off := false
	m.ai.settings.PlanFile = &off
	if m.ai.aiPlanDocOf(&ai.StoredMessage{Run: &ai.StoredRun{ID: "r1"}}, "x") != nil {
		t.Fatal("a plan doc with ai_plan_file off")
	}
}
