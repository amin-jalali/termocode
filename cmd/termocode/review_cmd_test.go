package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/prreview"
)

// reviewFakeAI answers every request with reply and keeps the requests.
type reviewFakeAI struct {
	reply string
	err   error
	reqs  []ai.Request
	off   bool
}

func (p *reviewFakeAI) Name() string        { return "fake" }
func (p *reviewFakeAI) Configured() bool    { return !p.off }
func (p *reviewFakeAI) SupportsTools() bool { return false }
func (p *reviewFakeAI) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	p.reqs = append(p.reqs, req)
	ch := make(chan ai.Chunk, 2)
	if p.err != nil {
		ch <- ai.Chunk{Done: true, Err: p.err}
	} else {
		ch <- ai.Chunk{Delta: p.reply}
		ch <- ai.Chunk{Done: true}
	}
	close(ch)
	return ch
}

// reviewRepo is a repository with main and a feature branch one commit
// ahead; AGENTS.md has a review section.
func reviewRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("AGENTS.md", "# Repo\nBuild it.\n## Review rules\nNever log tokens.\n")
	write("a.go", "package a\n")
	git("add", ".")
	git("commit", "-q", "-m", "one")
	git("checkout", "-q", "-b", "feature")
	write("a.go", "package a\n\nfunc F() { panic(1) }\n")
	git("commit", "-q", "-am", "two")
	return dir
}

func useReviewAI(t *testing.T, p *reviewFakeAI) {
	t.Helper()
	old := reviewProvider
	reviewProvider = func(func(string) string) (ai.Provider, ai.Config) { return p, ai.Config{Model: "m1"} }
	t.Cleanup(func() { reviewProvider = old })
}

type ghCall struct{ args []string }

func useReviewGH(t *testing.T, ids string) *[]ghCall {
	t.Helper()
	var calls []ghCall
	old := reviewGH
	reviewGH = func(_ string, args ...string) ([]byte, error) {
		calls = append(calls, ghCall{args})
		if len(args) > 1 && args[1] == "--paginate" {
			return []byte(ids), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { reviewGH = old })
	return &calls
}

func runReviewTest(t *testing.T, dir string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	code := runReviewIn(args, &out, &errw, func(k string) string { return env[k] }, func() (string, error) { return dir, nil })
	return code, out.String(), errw.String()
}

// CMP-36: the review sends the branch diff and the AGENTS.md review rules,
// prints only P0/P1 findings, and --fail-on sets the exit code.
func TestReviewPrintsHighPriorityFindings(t *testing.T) {
	dir := reviewRepo(t)
	p := &reviewFakeAI{reply: "- [P0] a.go:3 — F panics on every call\n- [P2] a.go:1 — style\n"}
	useReviewAI(t, p)
	code, out, errs := runReviewTest(t, dir, nil, "--base", "main")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if len(p.reqs) != 1 || p.reqs[0].Model != "m1" {
		t.Fatalf("requests %+v", p.reqs)
	}
	sent := p.reqs[0].Messages[1].Content
	for _, want := range []string{"panic(1)", "Never log tokens.", "main"} {
		if !strings.Contains(sent, want) {
			t.Errorf("prompt misses %q:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "Build it.") {
		t.Error("non-review rules sent")
	}
	if !strings.Contains(out, "**P0** `a.go:3`") || strings.Contains(out, "style") {
		t.Errorf("output:\n%s", out)
	}
	if code, _, _ := runReviewTest(t, dir, nil, "--base", "main", "--fail-on", "p0"); code != 1 {
		t.Errorf("--fail-on p0 with a P0: code %d", code)
	}
	code, out, _ = runReviewTest(t, dir, nil, "--base", "main", "--json")
	var js struct {
		Base     string             `json:"base"`
		Findings []prreview.Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &js); err != nil || code != 0 || js.Base != "main" || len(js.Findings) != 1 {
		t.Errorf("json %d %v %+v", code, err, js)
	}
}

// GITHUB_BASE_REF picks the base in GitHub Actions; no diff means no AI call.
func TestReviewBaseFromActionsAndEmptyDiff(t *testing.T) {
	dir := reviewRepo(t)
	p := &reviewFakeAI{reply: prreview.NoFindings}
	useReviewAI(t, p)
	code, _, errs := runReviewTest(t, dir, map[string]string{"GITHUB_BASE_REF": "main"})
	if code != 1 || !strings.Contains(errs, "origin/main") {
		t.Errorf("base from GITHUB_BASE_REF: code %d %s", code, errs)
	}
	code, out, _ := runReviewTest(t, dir, nil, "--base", "HEAD")
	if code != 0 || len(p.reqs) != 0 || !strings.Contains(out, "No high-priority problems") {
		t.Errorf("empty diff: code %d reqs %d\n%s", code, len(p.reqs), out)
	}
}

// --post updates the bot's earlier comment, or adds one.
func TestReviewPostsOneComment(t *testing.T) {
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{reply: prreview.NoFindings})
	calls := useReviewGH(t, "")
	code, _, errs := runReviewTest(t, dir, nil, "--base", "main", "--post", "#12")
	if code != 0 || len(*calls) != 2 {
		t.Fatalf("code %d calls %+v %s", code, *calls, errs)
	}
	post := strings.Join((*calls)[1].args, " ")
	if !strings.Contains(post, "-X POST") || !strings.Contains(post, "issues/12/comments") || !strings.Contains(post, "body=@") {
		t.Errorf("post %q", post)
	}

	calls = useReviewGH(t, "111\n222\n")
	if code, _, errs := runReviewTest(t, dir, nil, "--base", "main", "--post", "12"); code != 0 {
		t.Fatalf("update: %d %s", code, errs)
	}
	upd := strings.Join((*calls)[1].args, " ")
	if !strings.Contains(upd, "-X PATCH") || !strings.Contains(upd, "issues/comments/222") {
		t.Errorf("update %q", upd)
	}

	calls = useReviewGH(t, "not-a-number")
	if code, _, _ := runReviewTest(t, dir, nil, "--base", "main", "--post", "12"); code != 1 || len(*calls) != 1 {
		t.Errorf("bad id: code %d calls %d", code, len(*calls))
	}
}

// --post auto reads the pull request from the Actions event.
func TestReviewPostAuto(t *testing.T) {
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{reply: prreview.NoFindings})
	calls := useReviewGH(t, "")
	ev := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(ev, []byte(`{"pull_request":{"number":5}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := runReviewTest(t, dir, map[string]string{"GITHUB_EVENT_PATH": ev}, "--base", "main", "--post", "auto")
	if code != 0 || len(*calls) != 2 || !strings.Contains(strings.Join((*calls)[1].args, " "), "issues/5/comments") {
		t.Fatalf("code %d %s %+v", code, errs, *calls)
	}
	if code, _, errs := runReviewTest(t, dir, nil, "--post", "auto"); code != 1 || !strings.Contains(errs, "GitHub Actions") {
		t.Errorf("auto outside Actions: %d %s", code, errs)
	}
}

func TestReviewErrors(t *testing.T) {
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{off: true})
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--bogus"}, 2, "termocode review"},
		{[]string{"--base"}, 2, "termocode review"},
		{[]string{"--fail-on", "p2"}, 1, "--fail-on"},
		{[]string{"--base", "--evil"}, 1, "not a branch"},
		{[]string{"--post", "x"}, 1, "--post"},
		{[]string{"--base", "main"}, 1, "no AI provider"},
		{[]string{"--base", "nope"}, 1, "could not read"},
	}
	for _, c := range cases {
		code, _, errs := runReviewTest(t, dir, nil, c.args...)
		if code != c.code || !strings.Contains(errs, c.want) {
			t.Errorf("%v: code %d %q", c.args, code, errs)
		}
	}
	useReviewAI(t, &reviewFakeAI{err: context.DeadlineExceeded})
	if code, _, errs := runReviewTest(t, dir, nil, "--base", "main"); code != 1 || !strings.Contains(errs, "did not answer") {
		t.Errorf("AI error: %d %s", code, errs)
	}
	if code, out, _ := runReviewTest(t, dir, nil, "--help"); code != 0 || !strings.Contains(out, "--fail-on") {
		t.Errorf("help %d", code)
	}
	if code, _, errs := runReviewTest(t, t.TempDir(), nil, "--base", "main"); code != 1 || !strings.Contains(errs, "not in a git repository") {
		t.Errorf("no repo: %d %s", code, errs)
	}
}
