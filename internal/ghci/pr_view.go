package ghci

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// PRFile is one changed file of a pull request.
type PRFile struct {
	Path      string
	Additions int
	Deletions int
}

// PRReview is one review or review comment of a pull request.
type PRReview struct {
	Author string
	State  string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, "" for a plain comment
	Body   string
	When   string // ISO time as gh gives it
}

// PRDetail is a pull request with everything the view shows.
type PRDetail struct {
	PRItem
	State     string // OPEN, CLOSED, MERGED
	Body      string
	Mergeable string // MERGEABLE, CONFLICTING, UNKNOWN
	Files     []PRFile
	Reviews   []PRReview // reviews and comments, oldest first
}

const prViewFields = "number,title,body,author,state,isDraft,url,reviewDecision,statusCheckRollup," +
	"headRefName,baseRefName,mergeable,files,reviews,comments"

// ViewPR reads one pull request (`gh pr view N`).
func ViewPR(ctx context.Context, run Runner, dir string, number int) (PRDetail, error) {
	out, errOut, err := run(ctx, dir, "pr", "view", strconv.Itoa(number), "--json", prViewFields)
	if err != nil {
		return PRDetail{}, classify(err, errOut)
	}
	var r struct {
		prJSON
		Body   string `json:"body"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Head      string `json:"headRefName"`
		Base      string `json:"baseRefName"`
		Mergeable string `json:"mergeable"`
		Files     []struct {
			Path      string `json:"path"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"files"`
		Reviews []struct {
			Author      struct{ Login string } `json:"author"`
			State       string                 `json:"state"`
			Body        string                 `json:"body"`
			SubmittedAt string                 `json:"submittedAt"`
		} `json:"reviews"`
		Comments []struct {
			Author    struct{ Login string } `json:"author"`
			Body      string                 `json:"body"`
			CreatedAt string                 `json:"createdAt"`
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return PRDetail{}, fmt.Errorf("gh pr view: %w", err)
	}
	d := PRDetail{
		PRItem: PRItem{Number: r.Number, Title: r.Title, Author: r.Author.Login, Branch: r.Head, Base: r.Base, URL: r.URL,
			Draft: r.IsDraft, Review: r.ReviewDecision, Checks: checksOf(r.Rollup)},
		State: r.State, Body: r.Body, Mergeable: r.Mergeable,
	}
	for _, f := range r.Files {
		d.Files = append(d.Files, PRFile{Path: f.Path, Additions: f.Additions, Deletions: f.Deletions})
	}
	for _, v := range r.Reviews {
		d.Reviews = append(d.Reviews, PRReview{Author: v.Author.Login, State: v.State, Body: v.Body, When: v.SubmittedAt})
	}
	for _, c := range r.Comments {
		d.Reviews = append(d.Reviews, PRReview{Author: c.Author.Login, Body: c.Body, When: c.CreatedAt})
	}
	sortReviews(d.Reviews)
	return d, nil
}

// sortReviews orders by time (ISO strings sort as text); stable, so
// equal times keep gh's order.
func sortReviews(rs []PRReview) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].When < rs[j-1].When; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}
