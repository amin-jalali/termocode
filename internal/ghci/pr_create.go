package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DefaultBranch reads the default branch of the repository
// (`gh repo view`).
func DefaultBranch(ctx context.Context, run Runner, dir string) (string, error) {
	out, errOut, err := run(ctx, dir, "repo", "view", "--json", "defaultBranchRef")
	if err != nil {
		return "", classify(err, errOut)
	}
	var r struct {
		Ref struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Ref.Name == "" {
		return "", fmt.Errorf("gh repo view: no default branch")
	}
	return r.Ref.Name, nil
}

// Compose makes the title and body of a new pull request from the commit
// subjects of the branch, oldest first: the title is the first subject,
// the body lists all subjects (none for a single commit).
func Compose(subjects []string) (title, body string) {
	var clean []string
	for _, s := range subjects {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return "", ""
	}
	title = clean[0]
	if len(clean) == 1 {
		return title, ""
	}
	var b strings.Builder
	for _, s := range clean {
		b.WriteString("- " + s + "\n")
	}
	return title, strings.TrimRight(b.String(), "\n")
}

// NewPR is a pull request to create.
type NewPR struct {
	Title, Body  string
	Base, Branch string // Branch must be pushed
	Draft        bool
}

// Create opens a pull request (`gh pr create`) and returns its URL.
func Create(ctx context.Context, run Runner, dir string, p NewPR) (string, error) {
	if strings.TrimSpace(p.Title) == "" {
		return "", errors.New("a pull request needs a title")
	}
	if p.Branch == "" || strings.HasPrefix(p.Branch, "-") || strings.HasPrefix(p.Base, "-") {
		return "", errors.New("a pull request needs a branch")
	}
	args := []string{"pr", "create", "--title", p.Title, "--body", p.Body, "--head", p.Branch}
	if p.Base != "" {
		args = append(args, "--base", p.Base)
	}
	if p.Draft {
		args = append(args, "--draft")
	}
	out, errOut, err := run(ctx, dir, args...)
	if err != nil {
		return "", classify(err, errOut)
	}
	return lastURL(out), nil
}

// lastURL is the last http line of gh's output (the new PR's link).
func lastURL(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
			return l
		}
	}
	return ""
}
