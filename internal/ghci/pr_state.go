package ghci

import (
	"context"
	"errors"
	"regexp"
	"strconv"
)

// GITX-23: close / reopen a pull request and re-run its failed checks.

// SetClosed closes a pull request (closed true) or reopens it.
func SetClosed(ctx context.Context, run Runner, dir string, number int, closed bool) error {
	verb := "reopen"
	if closed {
		verb = "close"
	}
	_, errOut, err := run(ctx, dir, "pr", verb, strconv.Itoa(number))
	return classify(err, errOut)
}

// runURL finds the workflow run id in a check link.
var runURL = regexp.MustCompile(`/actions/runs/(\d+)(?:/|$)`)

// FailedRunIDs lists the workflow runs of the failed checks, once each,
// in the order of the checks. Checks from other CI services have none.
func FailedRunIDs(cs []Check) []int64 {
	var ids []int64
	seen := map[int64]bool{}
	for _, c := range cs {
		if c.State != StateFail {
			continue
		}
		mm := runURL.FindStringSubmatch(c.URL)
		if mm == nil {
			continue
		}
		id, err := strconv.ParseInt(mm[1], 10, 64)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// RerunFailed re-runs the failed jobs of each run (`gh run rerun ID
// --failed`); it stops at the first error.
func RerunFailed(ctx context.Context, run Runner, dir string, ids []int64) error {
	if len(ids) == 0 {
		return errors.New("no failed workflow run")
	}
	for _, id := range ids {
		if _, errOut, err := run(ctx, dir, "run", "rerun", strconv.FormatInt(id, 10), "--failed"); err != nil {
			return classify(err, errOut)
		}
	}
	return nil
}

// EditBody replaces the description of a pull request with the text of
// file path (`gh pr edit N --body-file path`): a long text never goes on
// the command line.
func EditBody(ctx context.Context, run Runner, dir string, number int, path string) error {
	if path == "" || path[0] == '-' {
		return errors.New("no description file")
	}
	_, errOut, err := run(ctx, dir, "pr", "edit", strconv.Itoa(number), "--body-file", path)
	return classify(err, errOut)
}
