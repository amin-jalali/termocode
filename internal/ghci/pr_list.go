package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	State  string // OPEN, CLOSED, MERGED (lists of closed or all PRs)
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

const prListFields = "number,title,author,headRefName,baseRefName,url,isDraft,state,reviewDecision,statusCheckRollup"

// PRFilter narrows the list (GITX-23). Zero is the open pull requests.
type PRFilter struct {
	State    string // open (""), closed, merged, all
	Author   string // a login or @me
	Assignee string
	Search   string // GitHub search words (label:bug review-requested:@me)
	Draft    bool   // drafts only
}

// ListPRs reads the open pull requests, newest first (`gh pr list`).
func ListPRs(ctx context.Context, run Runner, dir string) ([]PRItem, error) {
	return ListPRsFilter(ctx, run, dir, PRFilter{})
}

// ListPRsFilter reads the pull requests that match f, newest first.
func ListPRsFilter(ctx context.Context, run Runner, dir string, f PRFilter) ([]PRItem, error) {
	state := f.State
	switch state {
	case "":
		state = "open"
	case "open", "closed", "merged", "all":
	default:
		return nil, errors.New("unknown pull request state " + state)
	}
	args := []string{"pr", "list", "--state", state, "--limit", fmt.Sprint(ListLimit), "--json", prListFields}
	// "--flag=value": a value that starts with "-" (-label:wip) stays a value.
	if a := strings.TrimSpace(f.Author); a != "" {
		args = append(args, "--author="+a)
	}
	if a := strings.TrimSpace(f.Assignee); a != "" {
		args = append(args, "--assignee="+a)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		args = append(args, "--search="+q)
	}
	if f.Draft {
		args = append(args, "--draft")
	}
	out, errOut, err := run(ctx, dir, args...)
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
			URL: r.URL, Draft: r.IsDraft, State: r.State, Review: r.ReviewDecision, Checks: checksOf(r.Rollup)})
	}
	return items, nil
}

// MyPRs reads the open pull requests that ask for my review and the open
// ones I made (the "My Pull Requests" launchpad).
func MyPRs(ctx context.Context, run Runner, dir string) (review, mine []PRItem, err error) {
	review, err = ListPRsFilter(ctx, run, dir, PRFilter{Search: "review-requested:@me"})
	if err != nil {
		return nil, nil, err
	}
	mine, err = ListPRsFilter(ctx, run, dir, PRFilter{Author: "@me"})
	return review, mine, err
}
