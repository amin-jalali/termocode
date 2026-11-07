package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-55): an attach from another project never resolves in this
// hub: the send fails as a whole, and nothing of this project is read.
func TestWebAttachWSCheck(t *testing.T) {
	const own, other = "0123456789abcdef", "fedcba9876543210"
	ok := [][]webproto.Attach{
		nil,
		{{Kind: webproto.AttachFile, Path: "a.go"}},
		{{Kind: webproto.AttachFile, Path: "a.go", WS: own}, {Kind: webproto.AttachIssue, ID: "3"}},
	}
	for _, list := range ok {
		if err := webAttachWSCheck(list, own); err != nil {
			t.Errorf("%+v refused: %v", list, err)
		}
	}
	list := []webproto.Attach{{Kind: webproto.AttachFile, Path: "a.go", WS: own}, {Kind: webproto.AttachPR, ID: "7", WS: other}}
	err := webAttachWSCheck(list, own)
	var we *webError
	if !errors.As(err, &we) || we.code != webproto.CodeForbidden || !strings.Contains(we.msg, "another project") {
		t.Fatalf("other project's attach: %v", err)
	}
	// The hub's own key unknown: only home attaches go.
	if webAttachWSCheck([]webproto.Attach{{Kind: webproto.AttachIssue, ID: "3", WS: own}}, "") == nil {
		t.Error("a named project accepted by a hub without a key")
	}
}

func TestWebAttachProjectText(t *testing.T) {
	p := webproto.ProjectItem{ID: "0a1b2c3d4e5f", Name: "shop", Where: "~/code/shop", State: "running", Working: 2, Waiting: 1}
	got := webAttachProjectText(p, nil)
	for _, want := range []string{"shop", "~/code/shop", "running", "2 agents working", "1 waiting"} {
		if !strings.Contains(got, want) {
			t.Errorf("project text misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Branch") {
		t.Errorf("another project's text names a branch:\n%s", got)
	}
	here := webAttachProjectText(webproto.ProjectItem{Name: "termocode", Where: "~/termocode", State: "here"},
		&webAttachHere{branch: "main", ahead: 2, cards: 3, worktrees: 1})
	for _, want := range []string{"Branch: main (2 ahead)", "3 cards wait", "1 worktree"} {
		if !strings.Contains(here, want) {
			t.Errorf("this project's text misses %q:\n%s", want, here)
		}
	}
}

// fakeGH answers gh calls by their first two args.
func fakeGH(answers map[string]string, fail error) ghci.Runner {
	return func(_ context.Context, _ string, args ...string) (string, string, error) {
		if fail != nil {
			return "", "", fail
		}
		if len(args) < 2 {
			return "", "bad call", errors.New("exit 1")
		}
		out, ok := answers[args[0]+" "+args[1]]
		if !ok {
			return "", "no such call", errors.New("exit 1")
		}
		return out, "", nil
	}
}

func TestWebAttachGHText(t *testing.T) {
	run := fakeGH(map[string]string{
		"issue view": `{"number":12,"title":"Login breaks","state":"OPEN","url":"https://github.com/o/r/issues/12"}`,
		"pr view": `{"number":7,"title":"Fix login","state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/7",` +
			`"body":"Closes #12","author":{"login":"amin"},"headRefName":"fix/login","baseRefName":"main",` +
			`"files":[{"path":"auth.go","additions":3,"deletions":1}],"statusCheckRollup":[],"reviews":[],"comments":[]}`,
	}, nil)
	issue, err := webAttachGHText(context.Background(), run, "/w", webproto.AttachIssue, 12)
	if err != nil || !strings.Contains(issue, "Issue #12: Login breaks") || !strings.Contains(issue, "open") {
		t.Errorf("issue: %v\n%s", err, issue)
	}
	pr, err := webAttachGHText(context.Background(), run, "/w", webproto.AttachPR, 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pull request #7: Fix login", "fix/login → main", "auth.go (+3 −1)", "Closes #12"} {
		if !strings.Contains(pr, want) {
			t.Errorf("pr text misses %q:\n%s", want, pr)
		}
	}
	_, err = webAttachGHText(context.Background(), fakeGH(nil, ghci.ErrNoGitHub), "/w", webproto.AttachPR, 7)
	var we *webError
	if !errors.As(err, &we) || we.code != webproto.CodeConflict {
		t.Errorf("no GitHub remote: %v", err)
	}
}
