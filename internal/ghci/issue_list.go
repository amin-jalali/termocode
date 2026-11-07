package ghci

import (
	"context"
	"encoding/json"
	"fmt"
)

// Issue is one open issue of the repository (MOB-52: the phone's project
// tasks list them beside the checklists and pull requests).
type Issue struct {
	Number    int
	Title     string
	Author    string
	URL       string
	State     string   // OPEN
	Labels    []string // names, in gh's order
	Assignees []string // logins
	Updated   string   // ISO time
}

const issueListFields = "number,title,author,url,state,labels,assignees,updatedAt"

// ListIssues reads the open issues, newest first (`gh issue list`), at
// most ListLimit.
func ListIssues(ctx context.Context, run Runner, dir string) ([]Issue, error) {
	out, errOut, err := run(ctx, dir, "issue", "list", "--state", "open", "--limit", fmt.Sprint(ListLimit), "--json", issueListFields)
	if err != nil {
		return nil, classify(err, errOut)
	}
	type named struct {
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	var raw []struct {
		Number    int     `json:"number"`
		Title     string  `json:"title"`
		Author    named   `json:"author"`
		URL       string  `json:"url"`
		State     string  `json:"state"`
		Labels    []named `json:"labels"`
		Assignees []named `json:"assignees"`
		Updated   string  `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	items := make([]Issue, 0, len(raw))
	for _, r := range raw {
		it := Issue{Number: r.Number, Title: r.Title, Author: r.Author.Login, URL: r.URL, State: r.State, Updated: r.Updated}
		for _, l := range r.Labels {
			it.Labels = append(it.Labels, l.Name)
		}
		for _, a := range r.Assignees {
			it.Assignees = append(it.Assignees, a.Login)
		}
		items = append(items, it)
	}
	return items, nil
}
