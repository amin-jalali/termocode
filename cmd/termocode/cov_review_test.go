package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewArgumentAndEnvironmentErrors(t *testing.T) {
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{reply: "- [P2] a.go:1 — x\n"})
	for _, c := range []struct {
		args []string
		env  map[string]string
		code int
		err  string
	}{
		{[]string{"--base"}, nil, 2, "short AI review"},
		{[]string{"--post"}, nil, 2, "short AI review"},
		{[]string{"--fail-on"}, nil, 2, "short AI review"},
		{[]string{"--bogus"}, nil, 2, "short AI review"},
		{[]string{"--fail-on", "p9"}, nil, 1, "--fail-on must be"},
		{[]string{"--base", "-x"}, nil, 1, "is not a branch"},
		{[]string{"--post", "abc"}, nil, 1, "--post must be"},
		{[]string{"--post", "-3"}, nil, 1, "--post must be"},
		{[]string{"--post", "auto"}, nil, 1, "GITHUB_EVENT_PATH"},
		{[]string{"--post", "auto"}, map[string]string{"GITHUB_EVENT_PATH": filepath.Join(dir, "nope.json")}, 1, "termocode review:"},
		{[]string{"--base", "no-such-branch"}, nil, 1, "could not read the branch"},
	} {
		code, _, errw := runReviewTest(t, dir, c.env, c.args...)
		if code != c.code || !strings.Contains(errw, c.err) {
			t.Errorf("%v: %d %q", c.args, code, errw)
		}
	}
	// Not a git repository, no current folder.
	if code, _, errw := runReviewTest(t, t.TempDir(), nil, "--base", "main"); code != 1 || !strings.Contains(errw, "not in a git repository") {
		t.Errorf("no repo: %d %q", code, errw)
	}
	var out, errw strings.Builder
	if code := runReviewIn(nil, &out, &errw, func(string) string { return "" }, func() (string, error) { return "", errors.New("boom") }); code != 1 || !strings.Contains(errw.String(), "no current folder") {
		t.Errorf("no cwd: %d %q", code, errw.String())
	}
}

func TestReviewProviderProblemsAndJSON(t *testing.T) {
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{off: true})
	if code, _, errw := runReviewTest(t, dir, nil, "--base", "main"); code != 1 || !strings.Contains(errw, "no AI provider") {
		t.Errorf("unconfigured: %d %q", code, errw)
	}
	useReviewAI(t, &reviewFakeAI{err: errors.New("rate limited")})
	if code, _, errw := runReviewTest(t, dir, nil, "--base", "main"); code != 1 || !strings.Contains(errw, "did not answer") {
		t.Errorf("failing AI: %d %q", code, errw)
	}
	useReviewAI(t, &reviewFakeAI{reply: "- [P0] a.go:3 — bad\n"})
	code, out, _ := runReviewTest(t, dir, nil, "--base", "main", "--json", "--fail-on", "p0")
	if code != 1 || !strings.Contains(out, `"findings"`) || !strings.Contains(out, `"base": "main"`) {
		t.Errorf("json: %d %q", code, out)
	}
	// An empty diff gives an empty findings list in JSON.
	code, out, _ = runReviewTest(t, dir, nil, "--base", "HEAD", "--json")
	if code != 0 || !strings.Contains(out, `"findings": []`) {
		t.Errorf("empty json: %d %q", code, out)
	}
	// With no GITHUB_BASE_REF and no --base the default base is found.
	if code, _, _ := runReviewTest(t, dir, nil); code != 0 && code != 1 {
		t.Errorf("default base: %d", code)
	}
}

func TestPostReviewFailures(t *testing.T) {
	old := reviewGH
	t.Cleanup(func() { reviewGH = old })
	reviewGH = func(string, ...string) ([]byte, error) { return nil, errors.New("gh down") }
	if postReview(t.TempDir(), 7, "body") == nil {
		t.Fatal("a failing gh must fail the post")
	}
	reviewGH = func(_ string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "--paginate" {
			return []byte("not-a-number\n"), nil
		}
		return nil, nil
	}
	if err := postReview(t.TempDir(), 7, "body"); err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Fatalf("bad id: %v", err)
	}
	// A failed post is reported by the command.
	dir := reviewRepo(t)
	useReviewAI(t, &reviewFakeAI{reply: "- [P0] a.go:3 — bad\n"})
	reviewGH = func(string, ...string) ([]byte, error) { return nil, errors.New("gh down") }
	if code, _, errw := runReviewTest(t, dir, nil, "--base", "main", "--post", "#5"); code != 1 || !strings.Contains(errw, "could not post") {
		t.Errorf("post failure: %d %q", code, errw)
	}
}

// The default provider reads the settings and credentials of the user.
func TestDefaultReviewProviderAndGH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("PATH", t.TempDir())
	if err := os.MkdirAll(filepath.Join(home, "cfg"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, cfg := reviewProvider(func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "k"
		}
		return ""
	})
	if p == nil || cfg.Model == "" {
		t.Fatalf("provider %v cfg %+v", p, cfg)
	}
	if _, err := reviewGH(t.TempDir(), "api", "x"); err == nil {
		t.Fatal("gh is not installed here")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if out, err := reviewGH(t.TempDir(), "api", "x"); err != nil || !strings.Contains(string(out), "hello") {
		t.Fatalf("%q %v", out, err)
	}
}
