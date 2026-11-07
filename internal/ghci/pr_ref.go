package ghci

import "context"

// PRByRef reads one pull request with its checks and review: ref is a
// number, a link or a branch, as `gh pr view` takes it. A ref with no
// pull request gives nil and no error.
func PRByRef(ctx context.Context, run Runner, dir, ref string) (*PR, error) {
	return fetchPR(ctx, run, dir, ref)
}
