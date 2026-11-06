package ghci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ── Worktree links (WTX-9) ───────────────────────────────────────────────
//
// A worktree can be linked to an issue or a pull request: the user types
// a number, #N or a URL. A URL must be https on an allowed host (the
// repository's GitHub host) and is only read for its repository, kind
// and number: gh is always run with the number and, for a URL, --repo
// host/owner/repo, never with the text as typed.

// Kinds of a link.
const (
	LinkIssue = "issue"
	LinkPR    = "pr"
)

// LinkRef is what the user typed, checked.
type LinkRef struct {
	Kind   string // LinkIssue, LinkPR or "" (a bare number: either)
	Number int
	Host   string // a URL's host ("" for a number)
	Repo   string // a URL's owner/repo
}

// Link is a linked issue or pull request as gh reads it.
type Link struct {
	Kind   string
	Number int
	Title  string
	State  string // open, closed, merged
	CI     State  // pull requests: the checks; issues: StateNone
	URL    string
}

var (
	reLinkNumber = lazyre.New(`^#?([1-9][0-9]{0,8})$`)
	reLinkName   = lazyre.New(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
)

// ParseLinkRef checks s: a number, #N, or https://<host>/<owner>/<repo>/
// (pull|issues)/<N> on one of hosts.
func ParseLinkRef(s string, hosts []string) (LinkRef, error) {
	s = strings.TrimSpace(s)
	if m := reLinkNumber.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return LinkRef{Number: n}, nil
	}
	bad := errors.New("type an issue or pull request number, or its GitHub URL")
	if !strings.HasPrefix(s, "https://") {
		return LinkRef{}, bad
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Port() != "" {
		return LinkRef{}, bad
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, h := range hosts {
		if host == strings.ToLower(h) {
			allowed = true
		}
	}
	if !allowed {
		return LinkRef{}, fmt.Errorf("%s is not this repository's GitHub host (%s)", host, strings.Join(hosts, ", "))
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || !reLinkName.MatchString(parts[0]) || !reLinkName.MatchString(parts[1]) {
		return LinkRef{}, bad
	}
	ref := LinkRef{Host: host, Repo: parts[0] + "/" + parts[1]}
	switch parts[2] {
	case "pull":
		ref.Kind = LinkPR
	case "issues":
		ref.Kind = LinkIssue
	default:
		return LinkRef{}, bad
	}
	m := reLinkNumber.FindStringSubmatch(parts[3])
	if m == nil || strings.HasPrefix(parts[3], "#") {
		return LinkRef{}, bad
	}
	ref.Number, _ = strconv.Atoi(m[1])
	return ref, nil
}

// ViewLink reads the linked issue or pull request. A bare number is
// asked as a pull request first, then as an issue.
func ViewLink(ctx context.Context, run Runner, dir string, ref LinkRef) (Link, error) {
	if ref.Number <= 0 {
		return Link{}, errors.New("no number")
	}
	var repo []string
	if ref.Repo != "" {
		repo = []string{"--repo", ref.Host + "/" + ref.Repo}
	}
	n := strconv.Itoa(ref.Number)
	if ref.Kind != LinkIssue {
		out, errOut, err := run(ctx, dir, append([]string{"pr", "view", n, "--json", "number,title,state,url,statusCheckRollup"}, repo...)...)
		if err == nil {
			var p prJSON
			if err := json.Unmarshal([]byte(out), &p); err != nil {
				return Link{}, fmt.Errorf("gh pr view: %w", err)
			}
			return Link{Kind: LinkPR, Number: p.Number, Title: p.Title, State: strings.ToLower(p.State), CI: sumChecks(checksOf(p.Rollup)), URL: p.URL}, nil
		}
		low := strings.ToLower(errOut)
		notPR := strings.Contains(low, "could not resolve to a pullrequest") || strings.Contains(low, "no pull requests found")
		if ref.Kind == LinkPR || !notPR {
			return Link{}, classify(err, errOut)
		}
	}
	out, errOut, err := run(ctx, dir, append([]string{"issue", "view", n, "--json", "number,title,state,url"}, repo...)...)
	if err != nil {
		return Link{}, classify(err, errOut)
	}
	var i struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &i); err != nil {
		return Link{}, fmt.Errorf("gh issue view: %w", err)
	}
	return Link{Kind: LinkIssue, Number: i.Number, Title: i.Title, State: strings.ToLower(i.State), URL: i.URL}, nil
}
