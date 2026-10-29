package ghci

import (
	"context"
	"encoding/json"
	"fmt"
)

// PRItem is one row of the open pull requests of the repository.
type PRItem struct {
	Number int
	Title  string
	Author string
	Branch string // head branch
	Base   string // base branch
	URL    string
	Draft  bool
	Review string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Checks []Check
}

// ChecksState sums up the checks: failed wins over pending, pending over
// pass; no checks is StateNone.
func (p PRItem) ChecksState() State { return sumChecks(p.Checks) }

func sumChecks(cs []Check) State {
	st := StateNone
	for _, c := range cs {
		switch c.State {
		case StateFail:
			return StateFail
		case StatePending:
			st = StatePending
		case StatePass:
			if st == StateNone {
				st = StatePass
			}
		}
	}
	return st
}

// ListLimit caps the list (gh --limit).
const ListLimit = 50

const prListFields = "number,title,author,headRefName,baseRefName,url,isDraft,reviewDecision,statusCheckRollup"

// ListPRs reads the open pull requests, newest first (`gh pr list`).
func ListPRs(ctx context.Context, run Runner, dir string) ([]PRItem, error) {
	out, errOut, err := run(ctx, dir, "pr", "list", "--state", "open", "--limit", fmt.Sprint(ListLimit), "--json", prListFields)
	if err != nil {
		return nil, classify(err, errOut)
	}
	var raw []struct {
		prJSON
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Head string `json:"headRefName"`
		Base string `json:"baseRefName"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	items := make([]PRItem, 0, len(raw))
	for _, r := range raw {
		items = append(items, PRItem{Number: r.Number, Title: r.Title, Author: r.Author.Login, Branch: r.Head, Base: r.Base,
			URL: r.URL, Draft: r.IsDraft, Review: r.ReviewDecision, Checks: checksOf(r.Rollup)})
	}
	return items, nil
}
