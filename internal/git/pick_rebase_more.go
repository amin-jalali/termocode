package git

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// More cherry-pick, revert and rebase flows (GITX-16, GITX-17) and
// patches (GITX-25).

const logFormat = "--pretty=format:%h%x1f%s%x1f%an%x1f%at%x1f%d%x1e"

// refusesOption errors for a name git would read as an option.
func refusesOption(what, name string) error {
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%s %q is not valid", what, name)
	}
	return nil
}

// BranchOnlyCommits lists, oldest first, the commits of branch that HEAD
// lacks, leaving out merges and commits whose change HEAD already has.
func BranchOnlyCommits(dir, branch string, limit int) ([]LogEntry, error) {
	if err := refusesOption("branch", branch); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	out, err := runCombined(dir, "git", "log", logFormat, "--reverse", "--no-merges", "--cherry-pick", "--right-only",
		fmt.Sprintf("-n%d", limit), "--end-of-options", "HEAD..."+branch, "--")
	if err != nil {
		return nil, err
	}
	return parseLogRecords(out), nil
}

// CommitsNotOn lists, oldest first, the commits of HEAD that other
// lacks (what a rebase onto other replays).
func CommitsNotOn(dir, other string, limit int) ([]LogEntry, error) {
	if err := refusesOption("branch", other); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	out, err := runCombined(dir, "git", "log", logFormat, "--reverse", fmt.Sprintf("-n%d", limit), "--end-of-options", other+"..HEAD", "--")
	if err != nil {
		return nil, err
	}
	return parseLogRecords(out), nil
}

// CherryPickMany applies hashes in order (`git cherry-pick`). On a
// conflict it returns the conflicted paths with git's error; git keeps
// the rest of the list (cherry-pick --continue / --abort).
func CherryPickMany(dir string, hashes []string) ([]string, error) {
	if len(hashes) == 0 {
		return nil, errors.New("no commits to cherry-pick")
	}
	for _, h := range hashes {
		if err := refusesOption("commit", h); err != nil {
			return nil, err
		}
	}
	return runPick(dir, "cherry-pick", append([]string{"cherry-pick", "--no-edit"}, hashes...))
}

func runPick(dir, verb string, args []string) ([]string, error) {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil, nil
	}
	if conflicts := unmergedPaths(dir); len(conflicts) > 0 {
		return conflicts, fmt.Errorf("%s stopped: %d conflict(s)", verb, len(conflicts))
	}
	return nil, cmdError(err, out)
}

// MergeParents lists the parents of a merge commit, first parent first.
func MergeParents(dir, hash string) ([]LogEntry, error) {
	out, err := runCombined(dir, "git", "rev-list", "--parents", "-n1", "--end-of-options", hash, "--")
	if err != nil {
		return nil, err
	}
	f := strings.Fields(out)
	if len(f) < 3 {
		return nil, fmt.Errorf("%s is not a merge commit", hash)
	}
	var ps []LogEntry
	for _, p := range f[1:] {
		o, err := runCombined(dir, "git", "log", logFormat, "--no-walk", p, "--")
		if err != nil {
			return nil, err
		}
		ps = append(ps, parseLogRecords(o)...)
	}
	return ps, nil
}

// RevertMerge reverts a merge commit, keeping parent (1 = the branch it
// was merged into).
func RevertMerge(dir, hash string, parent int) ([]string, error) {
	ps, err := MergeParents(dir, hash)
	if err != nil {
		return nil, err
	}
	if parent < 1 || parent > len(ps) {
		return nil, fmt.Errorf("the merge has no parent %d", parent)
	}
	return runPick(dir, "revert", []string{"revert", "--no-edit", "-m", fmt.Sprint(parent), hash})
}

// diffBase is the commit to diff hash against: its first parent, or the
// empty tree for a root commit.
func diffBase(dir, hash string) string {
	if p, err := ParentOf(dir, hash); err == nil {
		return p
	}
	return emptyTree
}

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// ApplyFileFromCommit applies the changes one commit made to path to the
// working tree and the index (a three-way apply: a clash becomes a
// conflict). Nothing is committed.
func ApplyFileFromCommit(dir, hash, path string) error {
	if err := refusesOption("commit", hash); err != nil {
		return err
	}
	patch, err := runCombined(dir, "git", "diff", "--binary", diffBase(dir, hash), hash, "--", path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(patch) == "" {
		return fmt.Errorf("%s does not change %s", shortHash(hash), path)
	}
	return applyStdin(dir, patch, "apply", "--3way")
}

func applyStdin(dir, stdin string, args ...string) error {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return cmdError(err, out)
	}
	return nil
}

// RestoreFileFrom puts path back as it was in hash (working tree and
// index). When path has uncommitted changes they are first saved in the
// stash list; backup names that entry ("" when none was needed).
func RestoreFileFrom(dir, hash, path string) (backup string, err error) {
	if err := refusesOption("commit", hash); err != nil {
		return "", err
	}
	if spawnQuiet(dir, "cat-file", "-e", hash+":"+path) != nil {
		return "", fmt.Errorf("%s is not in %s", path, shortHash(hash))
	}
	if spawnQuiet(dir, "diff", "--quiet", "HEAD", "--", path) != nil {
		sha, err := runCombined(dir, "git", "stash", "create")
		if err != nil {
			return "", err
		}
		if sha = strings.TrimSpace(sha); sha != "" {
			msg := "termocode: before restoring " + path + " from " + shortHash(hash)
			if _, err := runCombined(dir, "git", "stash", "store", "-m", msg, sha); err != nil {
				return "", err
			}
			backup = "stash@{0}"
		}
	}
	if _, err := runCombined(dir, "git", "restore", "--source="+hash, "--staged", "--worktree", "--", path); err != nil {
		return backup, err
	}
	return backup, nil
}

// rebaseNoPlan runs a rebase whose todo git writes itself (the helper
// keeps it).
func rebaseNoPlan(dir string, opt RebaseOptions, args ...string) (RebaseOutcome, error) {
	if opt.Autostash {
		args = append(args[:1:1], append([]string{"--autostash"}, args[1:]...)...)
	} else if WorkTreeDirty(dir) {
		return RebaseOutcome{}, ErrRebaseDirty
	}
	planPath, cleanup, err := writeRebasePlan(rebasePlan{})
	if err != nil {
		return RebaseOutcome{}, err
	}
	defer cleanup()
	return runRebaseCmd(dir, opt, planPath, args...)
}

// RebaseOnto replays the current branch onto onto (a branch, tag or
// commit); keepMerges keeps merge commits (--rebase-merges).
func RebaseOnto(dir, onto string, keepMerges bool, opt RebaseOptions) (RebaseOutcome, error) {
	if err := refusesOption("branch", onto); err != nil {
		return RebaseOutcome{}, err
	}
	if _, err := ResolveCommitName(dir, onto); err != nil {
		return RebaseOutcome{}, err
	}
	args := []string{"rebase"}
	if keepMerges {
		args = append(args, "--rebase-merges")
	}
	return rebaseNoPlan(dir, opt, append(args, onto)...)
}

// ResolveCommitName is the full hash of a commit, branch or tag name.
func ResolveCommitName(dir, ref string) (string, error) {
	if err := refusesOption("commit", ref); err != nil {
		return "", err
	}
	out, err := runOut(dir, "git", "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no commit %s", ref)
	}
	return strings.TrimSpace(out), nil
}

// rowTodo is the todo from the parent of full (or from the root) to
// HEAD; full must be on the current branch.
func rowTodo(dir, full string) (RebaseTodo, int, error) {
	if spawnQuiet(dir, "merge-base", "--is-ancestor", full, "HEAD") != nil {
		return RebaseTodo{}, 0, fmt.Errorf("%s is not on the current branch", shortHash(full))
	}
	base, _ := ParentOf(dir, full)
	todo, err := LoadRebaseTodo(dir, base)
	if err != nil {
		return todo, 0, err
	}
	for i, it := range todo.Items {
		if it.FullHash == full {
			return todo, i, nil
		}
	}
	return todo, 0, fmt.Errorf("%s is not in the rebase range", shortHash(full))
}

// RewordCommit gives one commit a new full message.
func RewordCommit(dir, full, message string, opt RebaseOptions) (RebaseOutcome, error) {
	if strings.TrimSpace(message) == "" {
		return RebaseOutcome{}, errors.New("the message is empty")
	}
	todo, i, err := rowTodo(dir, full)
	if err != nil {
		return RebaseOutcome{}, err
	}
	todo.Items[i].Action, todo.Items[i].Message = RebaseReword, message
	return RunRebase(dir, todo, opt)
}

// DropCommit removes one commit; the newer ones stay.
func DropCommit(dir, full string, opt RebaseOptions) (RebaseOutcome, error) {
	todo, i, err := rowTodo(dir, full)
	if err != nil {
		return RebaseOutcome{}, err
	}
	todo.Items[i].Action = RebaseDrop
	return RunRebase(dir, todo, opt)
}

// SquashIntoPrevious melds one commit into the commit before it (both
// messages kept).
func SquashIntoPrevious(dir, full string, opt RebaseOptions) (RebaseOutcome, error) {
	parent, err := ParentOf(dir, full)
	if err != nil {
		return RebaseOutcome{}, fmt.Errorf("%s is the first commit: there is nothing to squash it into", shortHash(full))
	}
	todo, _, err := rowTodo(dir, parent)
	if err != nil {
		return RebaseOutcome{}, err
	}
	todo.Items[1].Action = RebaseSquash
	return RunRebase(dir, todo, opt)
}

// AutosquashPlan counts the fixup! / squash! / amend! commits on HEAD
// (the last 200) and finds the base a rebase needs ("" = from the root).
func AutosquashPlan(dir string) (n int, base string, err error) {
	out, err := runCombined(dir, "git", "log", "--format=%H%x1f%s", "-n200", "HEAD", "--")
	if err != nil {
		if !hasCommits(localGit(dir)) {
			return 0, "", nil
		}
		return 0, "", err
	}
	type row struct{ full, subject string }
	var rows []row
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if h, s, ok := strings.Cut(l, "\x1f"); ok {
			rows = append(rows, row{h, s})
		}
	}
	oldest := -1
	for i, r := range rows {
		target := r.subject
		found := false
		for _, p := range []string{"fixup! ", "squash! ", "amend! "} {
			if t, ok := strings.CutPrefix(target, p); ok {
				target, found = t, true
				break
			}
		}
		if !found {
			continue
		}
		n++
		for j := i + 1; j < len(rows); j++ {
			if rows[j].subject == target || strings.HasPrefix(rows[j].full, target) {
				oldest = max(oldest, j)
				break
			}
		}
	}
	if n == 0 || oldest < 0 {
		return n, "", nil
	}
	base, _ = ParentOf(dir, rows[oldest].full)
	return n, base, nil
}

// Autosquash folds the fixup! / squash! commits into their targets
// (`git rebase -i --autosquash`, git's own todo).
func Autosquash(dir string, opt RebaseOptions) (RebaseOutcome, error) {
	n, base, err := AutosquashPlan(dir)
	if err != nil {
		return RebaseOutcome{}, err
	}
	if n == 0 {
		return RebaseOutcome{}, errors.New("no fixup! commits")
	}
	if _, err := LoadRebaseTodo(dir, base); err != nil {
		return RebaseOutcome{}, err
	}
	args := []string{"rebase", "-i", "--autosquash"}
	if base == "" {
		args = append(args, "--root")
	} else {
		args = append(args, base)
	}
	return rebaseNoPlan(dir, opt, args...)
}

// CommitPatch is a commit as a mail patch (`git format-patch`).
func CommitPatch(dir, hash string) (string, error) {
	if err := refusesOption("commit", hash); err != nil {
		return "", err
	}
	return runCombined(dir, "git", "format-patch", "-1", "--stdout", hash, "--")
}

// ChangesPatch is every uncommitted change of tracked files (staged and
// not) as one patch against HEAD.
func ChangesPatch(dir string) (string, error) {
	return runCombined(dir, "git", "diff", "--binary", "HEAD", "--")
}

// CheckPatch says whether patch applies to the working tree, and what it
// changes (git apply --stat).
func CheckPatch(dir, patch string) (string, error) {
	if !strings.Contains(patch, "\n+++ ") && !strings.Contains(patch, "GIT binary patch") {
		return "", errors.New("this is not a patch")
	}
	if err := applyStdin(dir, patch, "apply", "--check"); err != nil {
		return "", err
	}
	cmd := spawn.Command("git", "apply", "--stat")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", cmdError(err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ApplyPatch applies patch to the working tree (nothing is committed).
func ApplyPatch(dir, patch string) error {
	return applyStdin(dir, patch, "apply")
}

func parseLogRecords(out string) []LogEntry {
	var entries []LogEntry
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		fields := strings.Split(rec, "\x1f")
		if rec == "" || len(fields) < 5 {
			continue
		}
		entries = append(entries, LogEntry{Hash: fields[0], Subject: fields[1], Author: fields[2],
			When: relativeDate(fields[3]), Refs: strings.TrimSpace(fields[4])})
	}
	return entries
}
