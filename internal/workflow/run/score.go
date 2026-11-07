package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/score"
)

// ErrBelowPass is the error of a must_pass score step whose score is
// under its pass mark. It is Permanent: trying again gives the same.
var ErrBelowPass = errors.New("the score is below the pass mark")

// score grades a score step (CMP-23). It only reads the scope: every
// check is an expression on what earlier steps made. A check that fails
// to evaluate counts 0 and keeps its error. A dry run grades too (it is
// pure) but never fails the step.
func (r *runner) score(s *workflow.Step, f *frame, st *StepState) (string, error) {
	sp := s.Score
	if sp == nil {
		return "", Permanent(errors.New("a score step without checks"))
	}
	sc := r.scope(f)
	checks := make([]score.Check, 0, len(sp.Checks))
	for _, c := range sp.Checks {
		ck := score.Check{Name: c.Name, Weight: c.Weight}
		var err error
		if c.Test != "" {
			var ok bool
			if ok, err = r.opts.Eval.Test(c.Test, sc); err == nil && ok {
				ck.Value = 1
			}
		} else {
			var v any
			if v, err = r.opts.Eval.Eval(c.Value, sc); err == nil {
				if f, isNum := v.(float64); isNum && c.Max > 0 {
					v = f / c.Max
				}
				ck.Value, err = score.Value(v)
			}
		}
		if err != nil {
			ck.Value, ck.Error = 0, capText(err.Error(), 200)
		}
		checks = append(checks, ck)
	}
	res := score.Grade(checks, sp.Pass)
	r.setOutput(f, s, scoreScope(res))
	r.edit(func() { st.Score = &res })
	if sp.MustPass && !res.Passed && !r.opts.Dry {
		msg := fmt.Sprintf("score %.2f < pass %.2f", res.Score, res.Pass)
		if failed := res.Failed(); len(failed) > 0 {
			msg += " (low: " + strings.Join(failed, ", ") + ")"
		}
		return "", Permanent(fmt.Errorf("%s: %w", msg, ErrBelowPass))
	}
	return StateDone, nil
}

// scoreScope is steps.<id> of a score step: ok (it passed), score, pass
// and checks (name, value, weight, error).
func scoreScope(res score.Result) map[string]any {
	checks := make([]any, len(res.Checks))
	for i, c := range res.Checks {
		checks[i] = map[string]any{"name": c.Name, "value": c.Value, "weight": c.Weight, "error": c.Error}
	}
	return map[string]any{"ok": res.Passed, "score": res.Score, "pass": res.Pass, "checks": checks}
}

// Scores lists every score result of a run: its steps and the steps of
// every each item, in no fixed order.
func (rf *RunFile) Scores() []score.Result {
	if rf == nil {
		return nil
	}
	var out []score.Result
	var walk func(map[string]*StepState)
	walk = func(steps map[string]*StepState) {
		for _, st := range steps {
			if st == nil {
				continue
			}
			if st.Score != nil {
				out = append(out, *st.Score)
			}
			if st.Each != nil {
				for _, is := range st.Each.Items {
					if is != nil {
						walk(is.Steps)
					}
				}
			}
		}
	}
	walk(rf.Steps)
	return out
}

// capText cuts s to n bytes on a rune boundary.
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
