package ghci

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// MergeMethod says how a pull request is merged.
type MergeMethod string

const (
	MergeSquash MergeMethod = "squash"
	MergeCommit MergeMethod = "merge"
	MergeRebase MergeMethod = "rebase"
)

// Merge merges a pull request (`gh pr merge N --squash|--merge|--rebase`).
// It never passes --admin (no bypass of branch rules), --auto or
// --delete-branch: the user merges, nothing else changes.
func Merge(ctx context.Context, run Runner, dir string, number int, method MergeMethod) error {
	var flag string
	switch method {
	case MergeSquash, MergeCommit, MergeRebase:
		flag = "--" + string(method)
	default:
		return errors.New("unknown merge method " + string(method))
	}
	_, errOut, err := run(ctx, dir, "pr", "merge", strconv.Itoa(number), flag)
	return classify(err, errOut)
}

// shaRe is a plain hex commit id.
var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// MergeHead merges a pull request only if its head is still sha
// (`gh pr merge N --METHOD --match-head-commit SHA`): a push after CI
// went green makes GitHub refuse the merge. Like Merge it never passes
// --admin, --auto or --delete-branch.
func MergeHead(ctx context.Context, run Runner, dir string, number int, method MergeMethod, sha string) error {
	switch method {
	case MergeSquash, MergeCommit, MergeRebase:
	default:
		return errors.New("unknown merge method " + string(method))
	}
	if !shaRe.MatchString(sha) {
		return fmt.Errorf("not a commit id: %q", sha)
	}
	_, errOut, err := run(ctx, dir, "pr", "merge", strconv.Itoa(number), "--"+string(method), "--match-head-commit", sha)
	return classify(err, errOut)
}

// ConflictPrompt asks the AI to bring base into branch and resolve the
// merge conflicts of pull request number, then commit (no push).
func ConflictPrompt(branch, base string, number int) string {
	return fmt.Sprintf("Pull request #%d (branch %s) has merge conflicts with %s.\n"+
		"Bring %s in: run `git fetch origin %s` and `git merge origin/%s`, then resolve each conflict "+
		"so both sides keep working (do not just take one side), and run the checks you can.\n"+
		"Find the conflict markers with git_status and git_diff; leave no markers behind.",
		number, branch, base, base, base, base)
}
