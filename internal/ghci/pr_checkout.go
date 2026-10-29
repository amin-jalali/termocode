package ghci

import (
	"context"
	"strconv"
)

// Checkout switches to the branch of a pull request (`gh pr checkout N`).
// The caller makes sure the work tree is clean first.
func Checkout(ctx context.Context, run Runner, dir string, number int) error {
	_, errOut, err := run(ctx, dir, "pr", "checkout", strconv.Itoa(number))
	return classify(err, errOut)
}
