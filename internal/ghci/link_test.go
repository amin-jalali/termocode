package ghci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// WTX-9: what a user may type to link a worktree: a number, #N, or a
// GitHub issue / pull request URL of an allowed host. Anything else is
// refused before gh runs.
func TestParseLinkRef(t *testing.T) {
	hosts := []string{"github.com", "git.example.com"}
	for in, want := range map[string]LinkRef{
		"412":                             {Number: 412},
		" #412 ":                          {Number: 412},
		"https://github.com/o/r/pull/12":  {Kind: LinkPR, Number: 12, Host: "github.com", Repo: "o/r"},
		"https://github.com/o/r/issues/7": {Kind: LinkIssue, Number: 7, Host: "github.com", Repo: "o/r"},
		"https://github.com/o/r.js/pull/12/files":     {Kind: LinkPR, Number: 12, Host: "github.com", Repo: "o/r.js"},
		"https://git.example.com/team/app/issues/3#x": {Kind: LinkIssue, Number: 3, Host: "git.example.com", Repo: "team/app"},
	} {
		got, err := ParseLinkRef(in, hosts)
		if err != nil || got != want {
			t.Errorf("ParseLinkRef(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "0", "-3", "abc", "12abc", "99999999999",
		"http://github.com/o/r/pull/12",              // not https
		"https://evil.example/o/r/pull/12",           // not an allowed host
		"https://github.com.evil.example/o/r/pull/1", // look-alike host
		"https://user:pw@github.com/o/r/pull/12",     // credentials in the URL
		"https://github.com/o/r/commit/12",           // not an issue or PR
		"https://github.com/o/r/pull/x",
		"https://github.com/-o/r/pull/1", // a name gh would read as a flag
		"https://github.com/o/r/pull/1?--repo=x",
		"--repo=x/y",
	} {
		if r, err := ParseLinkRef(bad, hosts); err == nil {
			t.Errorf("ParseLinkRef(%q) = %+v, want an error", bad, r)
		}
	}
}

// WTX-9: a pull request: its title, state and CI from one gh pr view;
// the number is passed alone and a URL's repository goes in --repo.
func TestViewLinkPR(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{"pr view": {out: `{"number":12,"title":"Add parser","state":"MERGED","url":"https://github.com/o/r/pull/12",
		"statusCheckRollup":[{"__typename":"CheckRun","name":"CI","status":"COMPLETED","conclusion":"SUCCESS"}]}`}}}
	l, err := ViewLink(context.Background(), f.run, "/r", LinkRef{Kind: LinkPR, Number: 12, Host: "github.com", Repo: "o/r"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != LinkPR || l.Number != 12 || l.Title != "Add parser" || l.State != "merged" || l.CI != StatePass || l.URL == "" {
		t.Errorf("link %+v", l)
	}
	got := strings.Join(f.calls[0], " ")
	if !strings.HasPrefix(got, "pr view 12 --json ") || !strings.HasSuffix(got, " --repo github.com/o/r") {
		t.Errorf("args %q", got)
	}
	if f.dirs[0] != "/r" {
		t.Errorf("dir %q", f.dirs[0])
	}
}

// A bare number that is not a pull request is read as an issue.
func TestViewLinkIssueFallback(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"pr view":    {errOut: "GraphQL: Could not resolve to a PullRequest with the number of 412. (repository.pullRequest)", err: errString("exit status 1")},
		"issue view": {out: `{"number":412,"title":"Pinned row in WORKTREES","state":"OPEN","url":"https://github.com/o/r/issues/412"}`},
	}}
	l, err := ViewLink(context.Background(), f.run, "/r", LinkRef{Number: 412})
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind != LinkIssue || l.Number != 412 || l.Title != "Pinned row in WORKTREES" || l.State != "open" || l.CI != StateNone {
		t.Errorf("link %+v", l)
	}
	if len(f.calls) != 2 || f.calls[1][0] != "issue" || strings.Contains(strings.Join(f.calls[1], " "), "--repo") {
		t.Errorf("calls %v", f.calls)
	}
	// An issue URL never asks for a pull request.
	f = &fakeGH{answers: map[string]fakeAnswer{"issue view": {out: `{"number":7,"title":"t","state":"CLOSED","url":"u"}`}}}
	if l, err := ViewLink(context.Background(), f.run, "/r", LinkRef{Kind: LinkIssue, Number: 7}); err != nil || l.State != "closed" || len(f.calls) != 1 {
		t.Errorf("issue url: %+v %v %v", l, err, f.calls)
	}
}

// No gh, no login: the quiet errors, so the UI shows one hint.
func TestViewLinkQuiet(t *testing.T) {
	noGH := func(context.Context, string, ...string) (string, string, error) { return "", "", ErrNoGH }
	if _, err := ViewLink(context.Background(), noGH, "/r", LinkRef{Number: 1}); !Quiet(err) {
		t.Errorf("no gh: %v", err)
	}
	f := &fakeGH{answers: map[string]fakeAnswer{"pr view": {errOut: "To get started with GitHub CLI, please run:  gh auth login", err: errString("exit 4")}}}
	if _, err := ViewLink(context.Background(), f.run, "/r", LinkRef{Number: 1}); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("not logged in: %v", err)
	}
	f = &fakeGH{answers: map[string]fakeAnswer{"pr view": {out: `not json`}}}
	if _, err := ViewLink(context.Background(), f.run, "/r", LinkRef{Kind: LinkPR, Number: 1}); err == nil {
		t.Error("bad JSON accepted")
	}
}
