package source

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGH is the fake gh runner.
type fakeGH struct {
	calls  [][]string
	dirs   []string
	out    string
	err    error
	stderr string
}

func (f *fakeGH) run(ctx context.Context, dir string, args ...string) (string, string, error) {
	f.calls = append(f.calls, args)
	f.dirs = append(f.dirs, dir)
	return f.out, f.stderr, f.err
}

func TestGitHubIssuesAndPulls(t *testing.T) {
	f := &fakeGH{out: `[{"number":12,"title":"a"},{"number":9,"title":"b"}]`}
	s := &Sources{Workspace: "/ws", GH: f.run}
	r, err := s.GitHub(context.Background(), GitHubInput{Kind: "issues", State: "open", Labels: []string{"bug"}, Repo: "o/r"})
	if err != nil || keys(r) != "12 9" {
		t.Fatalf("issues: %q %v", keys(r), err)
	}
	got := strings.Join(f.calls[0], " ")
	for _, want := range []string{"issue list", "--state open", "--label bug", "--repo o/r", "--json"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q miss %q", got, want)
		}
	}
	if f.dirs[0] != "/ws" {
		t.Errorf("dir %q", f.dirs[0])
	}
	r, err = s.GitHub(context.Background(), GitHubInput{Kind: "pulls"})
	if err != nil || len(r.Items) != 2 || !strings.HasPrefix(strings.Join(f.calls[1], " "), "pr list") {
		t.Fatalf("pulls: %v %v", f.calls[1], err)
	}
}

func TestGitHubChecksAndMaxItems(t *testing.T) {
	f := &fakeGH{out: `[{"name":"build","state":"SUCCESS"},{"name":"lint","state":"FAILURE"}]`}
	s := &Sources{GH: f.run}
	r, err := s.GitHub(context.Background(), GitHubInput{Kind: "checks", Number: 7, Shape: Shape{MaxItems: 1}})
	if err != nil || keys(r) != "build" || !r.Truncated || r.Total != 2 {
		t.Fatalf("checks: %+v %v", r, err)
	}
	if !strings.HasPrefix(strings.Join(f.calls[0], " "), "pr checks 7") {
		t.Errorf("args %v", f.calls[0])
	}
}

func TestGitHubRefusesBadInput(t *testing.T) {
	f := &fakeGH{out: `[]`}
	s := &Sources{GH: f.run}
	ctx := context.Background()
	for _, in := range []GitHubInput{
		{Kind: "delete"},
		{Kind: "issues", State: "--web"},
		{Kind: "issues", Repo: "--hostname=evil"},
		{Kind: "issues", Labels: []string{"--web"}},
		{Kind: "checks"},
		{Kind: "issues", Shape: Shape{MaxItems: 5000}},
	} {
		if _, err := s.GitHub(ctx, in); err == nil {
			t.Errorf("%+v: want an error", in)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("gh ran: %v", f.calls)
	}
	if _, err := (&Sources{}).GitHub(ctx, GitHubInput{Kind: "issues"}); err == nil {
		t.Error("no runner: want an error")
	}
}

func TestGitHubErrorsHaveNoToken(t *testing.T) {
	tok := "ghp_" + strings.Repeat("a", 36)
	f := &fakeGH{err: errors.New("exit 1"), stderr: "bad credentials " + tok + "\nmore"}
	s := &Sources{GH: f.run}
	_, err := s.GitHub(context.Background(), GitHubInput{Kind: "issues"})
	if err == nil || strings.Contains(err.Error(), tok) {
		t.Fatalf("err %v", err)
	}
	f = &fakeGH{out: `[{"number":1,"body":"` + tok + `"}]`}
	s = &Sources{GH: f.run}
	r, err := s.GitHub(context.Background(), GitHubInput{Kind: "issues"})
	if err != nil || strings.Contains(string(r.Items[0].Data), tok) {
		t.Fatalf("item leaks token: %v %v", r, err)
	}
}
