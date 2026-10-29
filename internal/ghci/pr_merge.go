package ghci

import (
	"context"
	"errors"
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
