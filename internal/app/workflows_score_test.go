package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/score"
)

// Spec (CMP-23): the Workflows view shows scores: the history row has
// the run's mean and passed count, an item row the score of its score
// step, and the step line says how many checks and the pass mark.
func TestWorkflowsViewShowsScores(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	pass := &score.Result{Score: 1, Pass: 0.7, Passed: true}
	fail := &score.Result{Score: 0.5, Pass: 0.7, Checks: []score.Check{{Name: "tests", Weight: 1, Value: 0.5}}}
	rf := &run.RunFile{ID: "r1", Workflow: "fix", State: run.RunOK, StartedAt: now.Add(-time.Hour), EndedAt: now,
		Steps: map[string]*run.StepState{
			"g": {State: run.StateDone, Score: pass},
			"fix": {State: run.StateDone, Each: &run.EachState{Items: []*run.ItemState{
				{Key: "a", State: run.StateFailed, Steps: map[string]*run.StepState{"g2": {State: run.StateFailed, Score: fail}}},
			}}},
		}}
	if row := wfxRunRow(rf, now).text; !strings.Contains(row, "score 0.75 (1/2)") {
		t.Errorf("history row %q", row)
	}
	if row := wfxItemRow(rf.Steps["fix"].Each.Items[0]).text; !strings.Contains(row, "g2 ✗") || !strings.Contains(row, "0.50 ✗") {
		t.Errorf("item row %q", row)
	}
	if w := wfxScoreWord(*pass); w != "1.00 ✓" {
		t.Errorf("word %q", w)
	}
	s := &workflow.Step{Type: workflow.TypeScore, Score: &workflow.ScoreSpec{Pass: 0.7, MustPass: true, Checks: make([]workflow.ScoreCheck, 3)}}
	if got := wfxStepWhat(s); got != "3 checks · pass 0.70 · must pass" {
		t.Errorf("what %q", got)
	}
	if !strings.Contains(aiWorkflowsNote, "score (") {
		t.Error("the workflow chat note does not teach the score step")
	}
}
