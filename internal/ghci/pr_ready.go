package ghci

import (
	"context"
	"strconv"
)

// SetDraft marks a pull request ready for review (draft false:
// `gh pr ready N`) or converts it back to a draft (`gh pr ready N --undo`).
func SetDraft(ctx context.Context, run Runner, dir string, number int, draft bool) error {
	args := []string{"pr", "ready", strconv.Itoa(number)}
	if draft {
		args = append(args, "--undo")
	}
	_, errOut, err := run(ctx, dir, args...)
	return classify(err, errOut)
}
