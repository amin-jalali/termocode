package git

import (
	"errors"
	"strings"
)

// UndoPlan is what an undo (or a move back to a reflog line) will do.
// termocode moves HEAD with `git reset --keep` (it refuses to lose local
// changes) and undoes a checkout with `git switch`; it never runs
// `reset --hard` and never force-pushes.
type UndoPlan struct {
	Entry         ReflogEntry // the action undone, or the reflog line picked
	Target        string      // full hash HEAD moves to
	TargetSubject string
	Switch        string // a checkout undo: the branch to switch back to ("" = reset)
	From          string // full hash of HEAD now (Redo comes back here)
	Branch        string // the current branch ("" when detached)
	Upstream      string // the branch's upstream ("origin/main"), "" without one
	Pushed        int    // commits leaving the branch that are on Upstream already
	Blocked       []string
	Staged        bool // staged changes: reset --keep leaves them unstaged
}

// TargetShort is the short hash HEAD moves to.
func (p UndoPlan) TargetShort() string { return shortHash(p.Target) }

// Noop reports whether HEAD is where the plan would put it already.
func (p UndoPlan) Noop() bool {
	if p.Switch != "" {
		return p.Switch == p.Branch
	}
	return p.Target == p.From
}

// BusyError: a git action is stopped half-way, so HEAD must not move.
type BusyError struct{ Op string }

func (e *BusyError) Error() string { return "a " + e.Op + " is in progress" }

var (
	// ErrNothingToUndo: the repo has no git action in its reflog.
	ErrNothingToUndo = errors.New("nothing to undo")
	// ErrUndoFirstCommit: the last action made the first commit; there is
	// nothing before it to go back to.
	ErrUndoFirstCommit = errors.New("this is the first commit; there is nothing before it")
)

// PlanUndo plans the undo of the last git action in dir: HEAD goes back
// to where it was before it; a checkout switches back to the branch left.
func PlanUndo(dir string) (UndoPlan, error) {
	if op := OperationInProgress(dir); op != "" {
		return UndoPlan{}, &BusyError{Op: op}
	}
	log, err := Reflog(dir, 1)
	if err != nil {
		return UndoPlan{}, err
	}
	if len(log) == 0 {
		return UndoPlan{}, ErrNothingToUndo
	}
	e := log[0]
	if e.Prev == "" {
		return UndoPlan{}, ErrUndoFirstCommit
	}
	p := UndoPlan{Entry: e, Target: e.Prev}
	if e.Kind == ReflogCheckout && isBranch(dir, e.From) {
		p.Switch = e.From
	}
	return finishPlan(dir, p)
}

// PlanMoveTo plans a move of HEAD (the current branch) to the commit the
// reflog line e left HEAD on.
func PlanMoveTo(dir string, e ReflogEntry) (UndoPlan, error) {
	if op := OperationInProgress(dir); op != "" {
		return UndoPlan{}, &BusyError{Op: op}
	}
	return finishPlan(dir, UndoPlan{Entry: e, Target: e.Hash})
}

// finishPlan fills in where HEAD is, the target's subject, what is
// pushed and which local changes are in the way.
func finishPlan(dir string, p UndoPlan) (UndoPlan, error) {
	out, err := runOut(dir, "git", "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return UndoPlan{}, ErrNothingToUndo
	}
	p.From = strings.TrimSpace(out)
	if out, err := runOut(dir, "git", "log", "-1", "--format=%s", p.Target, "--"); err == nil {
		p.TargetSubject = strings.TrimSpace(out)
	}
	if out, err := runOut(dir, "git", "branch", "--show-current"); err == nil {
		p.Branch = strings.TrimSpace(out)
	}
	to := p.Target
	if p.Switch == "" {
		p.Upstream, p.Pushed = RebasePushed(dir, p.Target)
	} else {
		to = "refs/heads/" + p.Switch // the switch goes to the branch's tip
	}
	p.Blocked = blockedBy(dir, to)
	p.Staged = spawnQuiet(dir, "diff", "--cached", "--quiet") != nil
	return p, nil
}

// blockedBy lists the files with local changes (staged or not) that
// differ between HEAD and target: moving there would have to overwrite
// them, so reset --keep and switch refuse.
func blockedBy(dir, target string) []string {
	local, err := runOut(dir, "git", "diff", "--name-only", "-z", "HEAD")
	if err != nil || local == "" {
		return nil
	}
	moved, err := runOut(dir, "git", "diff", "--name-only", "-z", "HEAD", target)
	if err != nil {
		return nil
	}
	changed := map[string]bool{}
	for _, f := range strings.Split(moved, "\x00") {
		changed[f] = f != ""
	}
	var out []string
	for _, f := range strings.Split(local, "\x00") {
		if changed[f] {
			out = append(out, f)
		}
	}
	return out
}

// isBranch reports whether name is a local branch.
func isBranch(dir, name string) bool {
	return name != "" && spawnQuiet(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name) == nil
}
