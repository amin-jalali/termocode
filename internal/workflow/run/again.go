package run

import (
	"errors"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// ErrNothingToRetry: every step and item of the run ended well.
var ErrNothingToRetry = errors.New("nothing to retry: every step and item ended well")

// Retry makes an ended run ready to run again from its first part that
// did not end well (failed, denied, stopped, paused or interrupted), for
// Options.Resume. The steps before that part are kept with their
// answers. In that step, an each keeps the items that are done and runs
// the others again; every step after it runs again (it may have used the
// missing answer). Write records that were denied are dropped (the gate
// asks again); every other record stays, so a write that went out is
// found by its marker and never sent twice. rf is not changed.
//
// It refuses a dry run (start a new one), a run that still works, and a
// run of another version of the file.
func Retry(rf *RunFile, file *workflow.File) (*RunFile, error) {
	if err := retryCheck(rf, file); err != nil {
		return nil, err
	}
	out := rf.clone()
	if !retryList(file.Steps, out.Steps) {
		return nil, ErrNothingToRetry
	}
	kept := out.Writes[:0]
	for _, w := range out.Writes {
		if w.State != StateDenied {
			kept = append(kept, w)
		}
	}
	out.Writes = kept
	if out.State == RunOK || out.State == RunFail || out.State == RunCancel {
		out.State = RunInterrupt // no longer ended well or badly: it runs again
	}
	return out, nil
}

// CanRetry tells, without a copy, whether Retry would give a run (the
// run page shows its verb).
func CanRetry(rf *RunFile, file *workflow.File) bool {
	if retryCheck(rf, file) != nil {
		return false
	}
	for i := range file.Steps {
		if badStep(&file.Steps[i], rf.Steps) {
			return true
		}
	}
	return false
}

func retryCheck(rf *RunFile, file *workflow.File) error {
	switch {
	case rf == nil || file == nil:
		return errors.New("no run to retry")
	case rf.Dry:
		return errors.New("a dry run is not retried; start a new dry run")
	case rf.State == RunRunning:
		return errors.New("the run still works; stop it first")
	case rf.Hash != file.Hash:
		return errors.New("the workflow file changed since this run started; start a new run")
	}
	return nil
}

// retryList resets the steps of one list from its first bad step on;
// false: every step ended well.
func retryList(steps []workflow.Step, states map[string]*StepState) bool {
	from := -1
	for i := range steps {
		if badStep(&steps[i], states) {
			from = i
			break
		}
	}
	if from < 0 {
		return false
	}
	for i := from; i < len(steps); i++ {
		s := &steps[i]
		key := stepKey(s)
		st := states[key]
		dropSteps(s.Else, states) // its else runs again with it
		if i == from && st != nil && st.Each != nil && s.Each != nil {
			st.State, st.Error, st.Attempts, st.Output = StatePending, "", 0, nil
			for _, is := range st.Each.Items {
				if is.State == StateDone {
					continue
				}
				is.State, is.Error = StatePending, ""
				if is.Steps == nil {
					is.Steps = map[string]*StepState{}
				}
				if !retryList(s.Each.Steps, is.Steps) {
					clearBad(is.Steps)
				}
			}
			continue
		}
		delete(states, key)
		if s.Each != nil {
			dropSteps(s.Each.Steps, states)
		}
	}
	return true
}

// badStep: the step, an item of it, or a step of its else did not end
// well.
func badStep(s *workflow.Step, states map[string]*StepState) bool {
	st := states[stepKey(s)]
	if st == nil {
		return false
	}
	if !finished(st.State) && st.State != StatePending {
		return true
	}
	if st.Each != nil {
		for _, is := range st.Each.Items {
			if is.State != StateDone {
				return true
			}
		}
	}
	for i := range s.Else {
		if badStep(&s.Else[i], states) {
			return true
		}
	}
	return false
}

// dropSteps forgets the states of a list and its else lists.
func dropSteps(steps []workflow.Step, states map[string]*StepState) {
	for i := range steps {
		delete(states, stepKey(&steps[i]))
		dropSteps(steps[i].Else, states)
	}
}

// clearBad forgets the steps of an item that did not end well (an item
// that failed outside its steps, e.g. its worktree).
func clearBad(states map[string]*StepState) {
	for k, st := range states {
		if st != nil && !finished(st.State) {
			delete(states, k)
		}
	}
}
