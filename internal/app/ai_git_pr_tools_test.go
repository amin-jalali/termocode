package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ghci"
)

// aiFakeGH is a gh that answers the pull request and CI reads with fixed
// JSON and records every call (no network, no real GitHub).
type aiFakeGH struct {
	mu    sync.Mutex
	calls []string
}

func (f *aiFakeGH) run(_ context.Context, _ string, args ...string) (string, string, error) {
	line := strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(line, "pr list"):
		return `[{"number":7,"title":"Add x","author":{"login":"ann"},"headRefName":"feat","baseRefName":"main","url":"https://github.com/o/r/pull/7",` +
			`"isDraft":false,"state":"OPEN","reviewDecision":"APPROVED","statusCheckRollup":[]}]`, "", nil
	case strings.HasPrefix(line, "pr view 7 "):
		return `{"number":7,"title":"Add x","body":"Adds x.","author":{"login":"ann"},"state":"OPEN","isDraft":false,"url":"https://github.com/o/r/pull/7",` +
			`"reviewDecision":"APPROVED","statusCheckRollup":[],"headRefName":"feat","headRefOid":"abc1234","baseRefName":"main","mergeable":"MERGEABLE",` +
			`"files":[{"path":"x.go","additions":3,"deletions":1}],"reviews":[{"author":{"login":"bob"},"state":"APPROVED","body":"ok","submittedAt":"2026-11-01"}],"comments":[]}`, "", nil
	case strings.HasPrefix(line, "pr view "):
		return "", "no pull requests found for branch", errors.New("exit 1")
	case strings.HasPrefix(line, "pr diff 7"):
		return "diff --git a/x.go b/x.go\n+new line\n", "", nil
	case strings.HasPrefix(line, "run list"):
		return `[{"databaseId":99,"workflowName":"ci","displayTitle":"Add x","status":"completed","conclusion":"failure","headSha":"abc1234","url":"https://github.com/o/r/actions/runs/99"}]`, "", nil
	case strings.HasPrefix(line, "run view 99 --log-failed"):
		return "build\ttest\t2026-11-01T00:00:00.0Z Error: boom\n", "", nil
	case strings.HasPrefix(line, "pr create"):
		return "https://github.com/o/r/pull/8\n", "", nil
	}
	return "", "", nil
}

func (f *aiFakeGH) ran(want string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, want) {
			return true
		}
	}
	return false
}

// Spec: git_pr does what the PR and CI cards do, through the laptop's gh
// runner: list, view, diff, threads, checks and ci_log run at once;
// create, checkout, merge, review, reply, resolve_thread, comment, draft,
// ready, close, reopen and rerun ask every time on a Cancel-first card,
// as the web's pr.merge / pr.review / pr.checkout cards do.
func TestAIGitPROps(t *testing.T) {
	m, dir := covGitRepo(t)
	aiRemoteRepo(t, dir)
	covGit(t, dir, "checkout", "-q", "-b", "feat")
	covGit(t, dir, "push", "-q", "-u", "origin", "feat")
	covRefresh(m)
	gh := &aiFakeGH{}
	m.ciW().run = ghci.Runner(gh.run)
	tl := aiGitToolAt(t, m, dir, "git_pr")
	for _, op := range []string{"list", "view", "diff", "threads", "checks", "ci_log"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	for _, op := range []string{"create", "checkout", "merge", "review", "reply", "resolve_thread", "comment", "draft", "ready", "close", "reopen", "rerun"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	if out := aiRun(t, tl, map[string]any{"op": "list"}); !strings.Contains(out, "#7 Add x") || !strings.Contains(out, "feat") {
		t.Errorf("list %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "view", "number": 7}); !strings.Contains(out, "MERGEABLE") || !strings.Contains(out, "x.go") || !strings.Contains(out, "bob") {
		t.Errorf("view %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "diff", "number": 7}); !strings.Contains(out, "+new line") {
		t.Errorf("diff %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "checks"}); !strings.Contains(out, "ci") || !strings.Contains(out, "fail") || !strings.Contains(out, "99") {
		t.Errorf("checks %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "ci_log", "run": 99}); !strings.Contains(out, "boom") {
		t.Errorf("ci_log %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "create", "title": "T", "body": "B", "base": "main"}); !strings.Contains(out, "pull/8") {
		t.Errorf("create %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "merge", "number": 7, "method": "squash"})
	aiRun(t, tl, map[string]any{"op": "review", "number": 7, "kind": "approve"})
	aiRun(t, tl, map[string]any{"op": "checkout", "number": 7})
	aiRun(t, tl, map[string]any{"op": "draft", "number": 7})
	aiRun(t, tl, map[string]any{"op": "close", "number": 7})
	aiRun(t, tl, map[string]any{"op": "rerun"})
	for _, want := range []string{"pr create --title T --body B --head feat --base main", "pr merge 7 --squash", "pr review 7 --approve",
		"pr checkout 7", "pr ready 7 --undo", "pr close 7", "run rerun 99 --failed"} {
		if !gh.ran(want) {
			t.Errorf("gh %q not run: %q", want, gh.calls)
		}
	}
	if _, err := aiRunErr(tl, map[string]any{"op": "review", "number": 7, "kind": "comment"}); err == nil {
		t.Error("a comment review with no body was sent")
	}
}
