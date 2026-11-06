package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Reviewers    []string // logins or org/team (GITX-23)
	Labels       []string
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
	if err := checkNames(p.Reviewers, true); err != nil {
		return "", err
	}
	if err := checkNames(p.Labels, false); err != nil {
		return "", err
	}
	if len(p.Reviewers) > 0 {
		args = append(args, "--reviewer", strings.Join(p.Reviewers, ","))
	}
	if len(p.Labels) > 0 {
		args = append(args, "--label", strings.Join(p.Labels, ","))
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

// checkNames refuses a name that gh would read as a flag or that would
// split the comma list; logins have no spaces either.
func checkNames(names []string, logins bool) error {
	for _, n := range names {
		if n == "" || strings.HasPrefix(n, "-") || strings.Contains(n, ",") || (logins && strings.ContainsAny(n, " \t")) {
			return errors.New("not a valid name: " + n)
		}
	}
	return nil
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

// ParseNames splits a typed list of names on commas; logins also split on
// spaces and lose a leading @. Empty names are dropped.
func ParseNames(s string, logins bool) []string {
	sep := func(r rune) bool { return r == ',' }
	if logins {
		sep = func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }
	}
	var out []string
	for _, n := range strings.FieldsFunc(s, sep) {
		n = strings.TrimSpace(n)
		if logins {
			n = strings.TrimPrefix(n, "@")
		}
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// prTemplatePlaces are where GitHub looks for a pull request template.
var prTemplatePlaces = []string{".github", "", "docs"}

// PRTemplate reads the repository's pull request template
// (pull_request_template.md in .github, the root or docs, any case). name
// is its path in the repository.
func PRTemplate(repo string) (text, name string, ok bool) {
	for _, place := range prTemplatePlaces {
		entries, err := os.ReadDir(filepath.Join(repo, place))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(e.Name(), "pull_request_template.md") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(repo, place, e.Name()))
			if err != nil || len(b) > 64<<10 {
				continue
			}
			return string(b), filepath.ToSlash(filepath.Join(place, e.Name())), true
		}
	}
	return "", "", false
}

// WithTemplate puts the template first and the composed body under it.
func WithTemplate(template, body string) string {
	t := strings.TrimRight(template, "\n\t ")
	switch {
	case t == "":
		return body
	case body == "":
		return t
	}
	return t + "\n\n" + body
}
