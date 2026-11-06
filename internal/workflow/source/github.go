package source

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHub source limits.
const DefaultGitHubTimeout = 60 * time.Second

// GitHubInput is a `"from": "github"` source. It reads only; the gh
// login is the token, so no secret is handled here.
type GitHubInput struct {
	// Kind is "issues", "pulls" or "checks" (the checks of pull Number).
	Kind string
	// Repo is "owner/name"; "" uses the workspace's own remote.
	Repo string
	// State is "open", "closed", "merged" (pulls) or "all"; "" is open.
	State string
	// Labels filters issues and pulls.
	Labels []string
	// Number is the pull request for checks.
	Number int
	// Timeout is for the caller's runner; the context carries it.
	Timeout time.Duration
	Shape
}

var (
	ghRepoRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9._-]{1,100}$`)
	ghLabelRe = regexp.MustCompile(`^[^-\x00-\x1f][^\x00-\x1f]{0,99}$`)
)

const (
	ghIssueFields = "number,title,state,author,labels,assignees,url,createdAt,updatedAt,body"
	ghPRFields    = "number,title,state,author,labels,headRefName,baseRefName,isDraft,reviewDecision,url,createdAt,updatedAt,body"
	ghCheckFields = "name,state,bucket,workflow,link,startedAt,completedAt"
)

// GitHub lists issues, pull requests or pull request checks through gh.
func (s *Sources) GitHub(ctx context.Context, in GitHubInput) (Result, error) {
	if s.GH == nil {
		return Result{}, errors.New("github: gh is not available")
	}
	sh := in.Shape
	if sh.Key == "" {
		sh.Key = "$.number"
		if in.Kind == "checks" {
			sh.Key = "$.name"
		}
	}
	col, err := newCollector(sh)
	if err != nil {
		return Result{}, fmt.Errorf("github: %w", err)
	}
	var args []string
	switch in.Kind {
	case "issues", "pulls":
		state := in.State
		if state == "" {
			state = "open"
		}
		switch state {
		case "open", "closed", "all":
		case "merged":
			if in.Kind != "pulls" {
				return Result{}, errors.New("github: state merged is for pulls only")
			}
		default:
			return Result{}, fmt.Errorf("github: state %q is not open, closed, merged or all", state)
		}
		if in.Kind == "issues" {
			args = []string{"issue", "list", "--json", ghIssueFields}
		} else {
			args = []string{"pr", "list", "--json", ghPRFields}
		}
		args = append(args, "--state", state, "--limit", strconv.Itoa(col.max+1))
		for _, l := range in.Labels {
			if !ghLabelRe.MatchString(l) {
				return Result{}, fmt.Errorf("github: label %q is not allowed", l)
			}
			args = append(args, "--label", l)
		}
	case "checks":
		if in.Number <= 0 {
			return Result{}, errors.New("github: checks needs a pull request number")
		}
		args = []string{"pr", "checks", strconv.Itoa(in.Number), "--json", ghCheckFields}
	default:
		return Result{}, fmt.Errorf("github: kind %q is not issues, pulls or checks", in.Kind)
	}
	if in.Repo != "" {
		if !ghRepoRe.MatchString(in.Repo) {
			return Result{}, fmt.Errorf("github: repo %q is not owner/name", in.Repo)
		}
		args = append(args, "--repo", in.Repo)
	}
	timeout := in.Timeout
	if timeout <= 0 {
		timeout = DefaultGitHubTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, errOut, err := s.GH(ctx, s.Workspace, args...)
	if err != nil {
		line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(errOut), "\n", 2)[0])
		if line == "" {
			line = err.Error()
		}
		return Result{}, errors.New(scrubShapes("github: " + in.Kind + ": " + line))
	}
	if err := col.addDoc([]byte(scrubShapes(out))); err != nil {
		return Result{}, errors.New(scrubShapes("github: " + err.Error()))
	}
	return col.res, nil
}
