package git

import (
	"errors"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// DiscardStashMessage names the safety stash Discard All and Delete
// Untracked Files leave behind, so the user finds it in the stash list.
const DiscardStashMessage = "termocode: safety copy before discard"

// ErrNothingToDiscard says a bulk discard found nothing to take away.
var ErrNothingToDiscard = errors.New("nothing to discard")

// hasHead reports whether the repo has a first commit.
func hasHead(dir string) bool {
	cmd := spawn.Command("git", "rev-parse", "--verify", "-q", "HEAD")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// UnstageAll takes every staged change out of the index; the working
// tree keeps it. With no commit yet (no HEAD to reset to) it removes
// everything from the index instead.
func UnstageAll(dir string) error {
	args := []string{"reset", "-q"}
	if !hasHead(dir) {
		args = []string{"rm", "-r", "-q", "--cached", "--ignore-unmatch", "--", "."}
	}
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// DiscardAll takes every tracked change (staged ones too) away by moving
// it into a new stash, the safety copy; untracked adds the new files.
// It returns the stash's ref ("stash@{0}"). Ignored files stay.
func DiscardAll(dir string, untracked bool) (string, error) {
	args := []string{"stash", "push", "-m", DiscardStashMessage}
	if untracked {
		args = append(args, "--include-untracked")
	}
	return safetyStash(dir, "", args...)
}

// DeleteUntracked moves only the untracked (not ignored) files into a new
// stash, the safety copy, and returns its ref. Tracked changes stay.
func DeleteUntracked(dir string) (string, error) {
	out, err := runOut(dir, "git", "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	if strings.Trim(out, "\x00") == "" {
		return "", ErrNothingToDiscard
	}
	return safetyStash(dir, out, "stash", "push", "-m", DiscardStashMessage,
		"--include-untracked", "--pathspec-from-file=-", "--pathspec-file-nul")
}

// safetyStash runs a stash push (stdin feeds a pathspec list) and
// reports the new stash, or ErrNothingToDiscard when git saved nothing.
func safetyStash(dir, stdin string, args ...string) (string, error) {
	before := stashTop(dir)
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", cmdError(err, out)
	}
	if after := stashTop(dir); after == "" || after == before {
		return "", ErrNothingToDiscard
	}
	return "stash@{0}", nil
}

// stashTop is the hash of stash@{0}, "" with no stash.
func stashTop(dir string) string {
	out, err := runOut(dir, "git", "rev-parse", "-q", "--verify", "refs/stash")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
