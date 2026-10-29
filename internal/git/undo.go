package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
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
	Detach        bool   // a checkout undo from a detached HEAD: go back to Target, detached
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
	if e.Kind == ReflogCheckout {
		if isBranch(dir, e.From) {
			p.Switch = e.From
		} else {
			p.Detach = true // the checkout left a detached HEAD
		}
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
	switch {
	case p.Switch != "":
		to = "refs/heads/" + p.Switch // the switch goes to the branch's tip
	case !p.Detach: // a detached HEAD leaves the branch as it is
		p.Upstream, p.Pushed = RebasePushed(dir, p.Target)
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

var (
	// ErrUndoLocalChanges: local changes are in the files the undo
	// changes; reset --keep (or switch) refused and nothing moved.
	ErrUndoLocalChanges = errors.New("your local changes are in the way")
	// ErrUndoStale: HEAD moved after the plan was made.
	ErrUndoStale = errors.New("HEAD moved since; look again")
)

// UndoOptions tune RunUndo.
type UndoOptions struct {
	Stash bool // stash local changes first and put them back after
}

// UndoResult is how an undo went.
type UndoResult struct {
	After     string // full hash of HEAD after the undo (Redo checks it)
	Stashed   bool   // local changes were stashed (and popped unless StashKept)
	StashKept bool   // they did not go back cleanly: the stash is still there
	StashRef  string // the stash that holds them ("stash@{0}") when kept
	PopError  string // what git said when the pop stopped
}

// undoStashMessage names the stash an undo makes.
const undoStashMessage = "termocode: before undo"

// RunUndo carries out p: `git reset --keep <target>` (or a switch back
// for a checkout). It refuses when HEAD moved since the plan, or when a
// git action is stopped half-way.
func RunUndo(dir string, p UndoPlan, opt UndoOptions) (UndoResult, error) {
	if op := OperationInProgress(dir); op != "" {
		return UndoResult{}, &BusyError{Op: op}
	}
	if headHash(dir) != p.From {
		return UndoResult{}, ErrUndoStale
	}
	if !opt.Stash || !WorkTreeDirty(dir) {
		if err := moveHead(dir, p); err != nil {
			return UndoResult{}, err
		}
		return UndoResult{After: headHash(dir)}, nil
	}
	if err := Stash(dir, undoStashMessage); err != nil {
		return UndoResult{}, err
	}
	if err := moveHead(dir, p); err != nil {
		_ = StashPop(dir) // put the changes back where they were
		return UndoResult{}, err
	}
	res := UndoResult{Stashed: true, After: headHash(dir)}
	if err := StashPop(dir); err != nil {
		// git keeps the stash when the pop stops on a conflict.
		res.StashKept, res.StashRef = true, "stash@{0}"
		res.PopError = err.Error()
	}
	return res, nil
}

// moveHead runs the reset --keep (or the switch back) of p. Both
// refuse instead of overwriting local changes.
func moveHead(dir string, p UndoPlan) error {
	args := []string{"reset", "--keep", p.Target}
	switch {
	case p.Switch != "":
		args = []string{"switch", "--no-guess", p.Switch}
	case p.Detach:
		args = []string{"switch", "--detach", p.Target}
	}
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		if WorkTreeDirty(dir) {
			return fmt.Errorf("%w: %w", ErrUndoLocalChanges, cmdError(err, out))
		}
		return cmdError(err, out)
	}
	return nil
}

// headHash is the full hash of HEAD ("" without commits).
func headHash(dir string) string {
	out, err := runOut(dir, "git", "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// RunRedo takes back the undo of p: HEAD goes back to p.From (or the
// switch goes back to p.Branch). after is HEAD right after the undo; when
// HEAD (or the branch) changed since, Redo refuses with ErrUndoStale.
func RunRedo(dir string, p UndoPlan, after string) error {
	if op := OperationInProgress(dir); op != "" {
		return &BusyError{Op: op}
	}
	branch, _ := runOut(dir, "git", "branch", "--show-current")
	want := p.Branch
	switch {
	case p.Switch != "":
		want = p.Switch
	case p.Detach:
		want = ""
	}
	if headHash(dir) != after || strings.TrimSpace(branch) != want {
		return ErrUndoStale
	}
	back := UndoPlan{Target: p.From}
	if p.Detach && p.Branch != "" {
		back.Switch = p.Branch
	} else if p.Switch != "" {
		if p.Branch == "" {
			return errors.New("can't redo: the undo left a detached HEAD")
		}
		back.Switch = p.Branch
	}
	return moveHead(dir, back)
}
