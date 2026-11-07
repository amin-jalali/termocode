package git

import (
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/clock"
)

// Stash options and actions (GITX-10), reset modes and uncommit
// (GITX-15).

// StashOptions tune StashPush.
type StashOptions struct {
	Message    string
	Untracked  bool     // -u: new files too
	StagedOnly bool     // --staged: only what is staged
	Paths      []string // only these files
}

// StashPush runs `git stash push` with the options. Nothing to stash is
// an error.
func StashPush(dir string, o StashOptions) error {
	args := stashPushArgs(o)
	before := StashHash(dir, "stash@{0}")
	if err := gitRun(dir, args...); err != nil {
		return err
	}
	if StashHash(dir, "stash@{0}") == before {
		return fmt.Errorf("no local changes to save")
	}
	return nil
}

// stashPushArgs is the `git stash push` argv (without "git") for o.
func stashPushArgs(o StashOptions) []string {
	args := []string{"stash", "push"}
	if o.Message != "" {
		args = append(args, "-m", o.Message)
	}
	if o.Untracked {
		args = append(args, "--include-untracked")
	}
	if o.StagedOnly {
		args = append(args, "--staged")
	}
	if len(o.Paths) > 0 {
		args = append(append(args, "--"), o.Paths...)
	}
	return args
}

// StashHash is the full hash of a stash ("" when there is none).
func StashHash(dir, ref string) string {
	out, err := runOut(dir, "git", "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// StashDiff is a stash's changes as a patch, new files included.
func StashDiff(dir, ref string) (string, error) {
	out, err := runCombined(dir, "git", "stash", "show", "--no-color", "-p", "--include-untracked", ref)
	if err != nil {
		return runCombined(dir, "git", "stash", "show", "--no-color", "-p", ref)
	}
	return out, nil
}

// StashStore puts a dropped stash (by its hash) back on the list.
func StashStore(dir, hash, message string) error {
	return gitRun(dir, "stash", "store", "-m", message, hash)
}

// StashBranch makes a branch at the stash's base commit, switches to it,
// applies the stash and drops it.
func StashBranch(dir, name, ref string) error {
	return gitRun(dir, "stash", "branch", name, ref)
}

// Reset modes.
const (
	ResetSoft  = "soft"  // keep the changes staged
	ResetMixed = "mixed" // keep the changes, unstaged
	ResetKeep  = "keep"  // keep local changes; refuse when they are in the way
	ResetHard  = "hard"  // drop everything (ResetHardSafe saves it first)
)

// Reset moves the current branch to rev with a soft, mixed or keep
// reset. A hard reset goes through ResetHardSafe.
func Reset(dir, mode, rev string) error {
	switch mode {
	case ResetSoft, ResetMixed, ResetKeep:
	default:
		return fmt.Errorf("unknown reset mode %q", mode)
	}
	return gitRun(dir, "reset", "-q", "--"+mode, rev, "--")
}

// ResetBackup is what ResetHardSafe saved: a ref at the old HEAD, and
// whether the uncommitted changes went to a stash.
type ResetBackup struct {
	Ref     string
	Old     string // short hash of the old HEAD
	Stashed bool
}

// ResetHardSafe saves HEAD as refs/termocode/backup/<time> and the
// uncommitted changes (untracked files too) as a stash, then resets hard
// to rev.
func ResetHardSafe(dir, rev string) (ResetBackup, error) {
	old := StashHash(dir, "HEAD")
	if old == "" {
		return ResetBackup{}, fmt.Errorf("no commit to reset")
	}
	b := ResetBackup{Ref: "refs/termocode/backup/" + clock.Now().UTC().Format("20060102-150405.000"), Old: old[:7]}
	b.Ref = strings.ReplaceAll(b.Ref, ".", "-")
	if err := gitRun(dir, "update-ref", b.Ref, old); err != nil {
		return ResetBackup{}, err
	}
	if st, _ := runOut(dir, "git", "status", "--porcelain"); strings.TrimSpace(st) != "" {
		if err := StashPush(dir, StashOptions{Message: "termocode: before reset --hard to " + rev, Untracked: true}); err != nil {
			return b, fmt.Errorf("could not save the changes first, nothing was reset: %w", err)
		}
		b.Stashed = true
	}
	return b, gitRun(dir, "reset", "-q", "--hard", rev, "--")
}

// Uncommit undoes the last commit and keeps its changes staged.
func Uncommit(dir string) error {
	if StashHash(dir, "HEAD~1") == "" {
		return fmt.Errorf("the first commit has no commit before it")
	}
	return Reset(dir, ResetSoft, "HEAD~1")
}
