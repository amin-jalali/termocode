package run

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// Spec (CMP-23): a score step grades earlier steps: tests give 1/0,
// values are divided by max and kept in 0..1, the score is the weighted
// mean, a broken check counts 0 with its error, and later steps read
// steps.<id>.ok / .score.
func TestScoreStepGrades(t *testing.T) {
	src := `{"version": 1, "name": "w", "vars": {"half": 0.5, "rating": 7}, "steps": [
	  {"id": "t", "type": "tests", "task": "test"},
	  {"id": "g", "type": "score", "pass": 0.6, "checks": [
	    {"name": "tests", "test": "steps.t.ok", "weight": 2},
	    {"name": "half", "value": "vars.half"},
	    {"name": "rating", "value": "vars.rating", "max": 10},
	    {"name": "broken", "value": "boom"}
	  ]},
	  {"id": "after", "type": "shell", "argv": ["echo"], "when": "steps.g.ok"}
	]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	g := rf.Steps["g"]
	if g == nil || g.State != StateDone || g.Score == nil {
		t.Fatalf("score step %+v", g)
	}
	// (2*1 + 0.5 + 0.7 + 0) / 5 = 0.64
	if s := g.Score; s.Score != 0.64 || s.Pass != 0.6 || !s.Passed || len(s.Checks) != 4 {
		t.Fatalf("result %+v", s)
	}
	if c := g.Score.Checks[2]; c.Value != 0.7 {
		t.Errorf("rating check %+v", c)
	}
	if c := g.Score.Checks[3]; c.Value != 0 || !strings.Contains(c.Error, "boom") {
		t.Errorf("broken check %+v", c)
	}
	want := []string{`run t prompt="" wt=`, `run after prompt="" wt=`}
	if got := fk.all(); !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if got := rf.Scores(); len(got) != 1 || got[0].Score != 0.64 {
		t.Errorf("scores %+v", got)
	}
}

// Spec: with must_pass a score under the pass mark fails the step (no
// retry), its else runs, and the result stays in the run file.
func TestScoreStepMustPassRunsElse(t *testing.T) {
	src := `{"version": 1, "name": "w", "vars": {"half": 0.5}, "retries": {"max": 2}, "steps": [
	  {"id": "g", "type": "score", "pass": 0.9, "must_pass": true,
	   "checks": [{"name": "half", "value": "vars.half"}, {"name": "ok", "test": "true"}],
	   "else": [{"id": "fallback", "type": "shell", "argv": ["echo"]}]}
	]}`
	fk := newFakes()
	rf := wait(t, Start(context.Background(), mustParse(t, src), fk.options()))
	if rf.State != RunFail {
		t.Fatalf("state %q", rf.State)
	}
	g := rf.Steps["g"]
	if g.State != StateFailed || g.Attempts != 1 || g.Score == nil || g.Score.Score != 0.75 || g.Score.Passed {
		t.Fatalf("score step %+v %+v", g, g.Score)
	}
	if !strings.Contains(g.Error, "score 0.75 < pass 0.90") || !strings.Contains(g.Error, "low: half") || !strings.Contains(g.Error, ErrBelowPass.Error()) {
		t.Errorf("error %q", g.Error)
	}
	if got := fk.all(); !slices.Equal(got, []string{`run fallback prompt="" wt=`}) {
		t.Fatalf("calls %q", got)
	}
}

// Spec: a dry run grades too (it is pure) but never fails the step;
// Scores collects the results of every each item.
func TestScoreStepInEachDryRun(t *testing.T) {
	src := `{"version": 1, "name": "w", "steps": [
	  {"id": "bugs", "type": "source", "from": "github", "what": "issues"},
	  {"id": "fix", "type": "each", "items": "steps.bugs.items", "steps": [
	    {"id": "g", "type": "score", "pass": 0.5, "must_pass": true, "checks": [{"name": "ready", "test": "item.ready"}]}
	  ]}
	]}`
	fk := newFakes(items("a", "b")...)
	opts := fk.options()
	opts.Dry = true
	rf := wait(t, Start(context.Background(), mustParse(t, src), opts))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	got := rf.Scores()
	if len(got) != 2 {
		t.Fatalf("scores %+v", got)
	}
	passed := 0
	for _, r := range got {
		if r.Passed {
			passed++
		}
	}
	if passed != 1 {
		t.Errorf("passed %d of %+v", passed, got)
	}
	b := rf.Steps["fix"].Each.Items[1]
	if b.State != StateDone || b.Steps["g"].Score == nil || b.Steps["g"].Score.Score != 0 {
		t.Errorf("item b %+v", b)
	}
	var nilRF *RunFile
	if nilRF.Scores() != nil {
		t.Error("nil run file")
	}
	if !errors.Is(Permanent(ErrBelowPass), ErrBelowPass) {
		t.Error("ErrBelowPass does not unwrap")
	}
}
