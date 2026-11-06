package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// WFX-13 in the app: a workflow's pr write pushes the item's branch and
// opens the PR with gh; github.comment comments with gh; a resumed write
// is found by its marker in the PR bodies / comments. Never the real
// gh: a fake runner records the calls.

type fakeGH struct {
	mu    sync.Mutex
	calls []string
	reply map[string]string // first args word pair ("pr create") → stdout
}

func (f *fakeGH) run(_ context.Context, dir string, args ...string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dir+": "+strings.Join(args, " "))
	if len(args) >= 2 {
		if out, ok := f.reply[args[0]+" "+args[1]]; ok {
			return out, "", nil
		}
	}
	return "", "", nil
}

func TestWorkflowGitHubPRPushesThenCreates(t *testing.T) {
	gh := &fakeGH{reply: map[string]string{"pr create": "Creating…\nhttps://github.example/o/r/pull/7\n"}}
	var pushed []string
	g := &wfxGitHub{dir: "/ws", gh: gh.run, push: func(dir, branch string) error { pushed = append(pushed, dir+" "+branch); return nil }}
	res, err := g.PR(context.Background(), wire.PRCall{Dir: "/wt", Branch: "termocode/agent-x", Base: "main", Title: "fix: a", Body: "b\n\n<!-- m -->", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExternalID != "https://github.example/o/r/pull/7" {
		t.Errorf("url %q", res.ExternalID)
	}
	if len(pushed) != 1 || pushed[0] != "/wt termocode/agent-x" {
		t.Errorf("pushed %v", pushed)
	}
	if len(gh.calls) != 1 || !strings.HasPrefix(gh.calls[0], "/wt: pr create --title fix: a --body b\n\n<!-- m --> --head termocode/agent-x --base main --draft") {
		t.Errorf("gh %q", gh.calls)
	}

	// A failed push opens no PR.
	g.push = func(string, string) error { return errors.New("rejected") }
	gh.calls = nil
	if _, err := g.PR(context.Background(), wire.PRCall{Dir: "/wt", Branch: "b", Title: "t"}); err == nil || len(gh.calls) != 0 {
		t.Errorf("push failed: %v, gh %q", err, gh.calls)
	}
}

func TestWorkflowGitHubComment(t *testing.T) {
	gh := &fakeGH{reply: map[string]string{"pr comment": "https://github.example/o/r/pull/7#issuecomment-1\n"}}
	g := &wfxGitHub{dir: "/ws", gh: gh.run}
	res, err := g.Comment(context.Background(), "#7", "looks good\n\n<!-- m -->")
	if err != nil || res.ExternalID != "https://github.example/o/r/pull/7#issuecomment-1" {
		t.Fatalf("%+v %v", res, err)
	}
	if gh.calls[0] != "/ws: pr comment 7 --body looks good\n\n<!-- m -->" {
		t.Errorf("gh %q", gh.calls[0])
	}
	for _, bad := range []string{"", "x", "-1", "--repo=evil/x"} {
		if _, err := g.Comment(context.Background(), bad, "b"); err == nil {
			t.Errorf("pr %q: no error", bad)
		}
	}
}

func TestWorkflowGitHubFindByMarker(t *testing.T) {
	gh := &fakeGH{reply: map[string]string{
		"pr list": `[{"url":"https://github.example/o/r/pull/1","body":"other"},{"url":"https://github.example/o/r/pull/2","body":"x <!-- termocode-run:r1/pr/a -->"}]`,
		"pr view": `{"comments":[{"url":"https://github.example/c/1","body":"no"},{"url":"https://github.example/c/2","body":"y <!-- termocode-run:r1/note/a -->"}]}`,
	}}
	g := &wfxGitHub{dir: "/ws", gh: gh.run}
	res, ok, err := g.Find(context.Background(), wfxSpec("pr", ""), "termocode-run:r1/pr/a")
	if err != nil || !ok || res.ExternalID != "https://github.example/o/r/pull/2" {
		t.Errorf("pr: %+v %v %v", res, ok, err)
	}
	res, ok, err = g.Find(context.Background(), wfxSpec("github.comment", "7"), "termocode-run:r1/note/a")
	if err != nil || !ok || res.ExternalID != "https://github.example/c/2" {
		t.Errorf("comment: %+v %v %v", res, ok, err)
	}
	if _, ok, _ := g.Find(context.Background(), wfxSpec("pr", ""), "termocode-run:r1/pr/zzz"); ok {
		t.Error("found a marker that is not there")
	}
}

func TestWorkflowRunSettings(t *testing.T) {
	withConfigDir(t)
	if p, sb := wfxRunSettings(); p != 2 || sb != "worktree" {
		t.Errorf("defaults %d %q", p, sb)
	}
	if err := saveHTTPSetting("workflows_parallel_max", 5); err != nil {
		t.Fatal(err)
	}
	if err := saveHTTPSetting("workflows_agent_sandbox", "read-only"); err != nil {
		t.Fatal(err)
	}
	if p, sb := wfxRunSettings(); p != 5 || sb != "read-only" {
		t.Errorf("saved %d %q", p, sb)
	}
}

func wfxSpec(to, pr string) workflow.WriteSpec { return workflow.WriteSpec{To: to, PR: pr} }
