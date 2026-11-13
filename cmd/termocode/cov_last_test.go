//go:build unix

package main

import (
	"bytes"
	"github.com/amin-jalali/termocode/internal/workflow/score"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/secrets"
)

func TestCleanLineAndAlreadyRuns(t *testing.T) {
	if got := cleanLine("a\x1b[31mb\x7fc\u0085d"); got != "a[31mbcd" {
		t.Fatalf("%q", got)
	}
	var out bytes.Buffer
	webAlreadyRuns(&out, 4000, "https://lan.example:4000/\x1b")
	if !strings.Contains(out.String(), "https://lan.example:4000/") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("%q", out.String())
	}
	out.Reset()
	webAlreadyRuns(&out, 4000, "")
	if !strings.Contains(out.String(), "http://127.0.0.1:4000/") {
		t.Fatalf("%q", out.String())
	}
}

// A connections file that cannot be written fails add and remove with the error.
func TestDBWritesFailInAReadOnlyFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "termocode", "db", "connections.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"connections":[{"id":"pg","driver":"postgres","host":"db","port":5432}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	if code, _, errw := dbRun(`{"id":"my","driver":"mysql","host":"m","port":3306}`, "add"); code != 1 || !strings.Contains(errw, "termocode db:") {
		t.Errorf("add: %d %q", code, errw)
	}
	if code, _, errw := dbRun("", "remove", "pg"); code != 1 || !strings.Contains(errw, "termocode db:") {
		t.Errorf("remove: %d %q", code, errw)
	}
}

// With no main or master branch and no origin, review asks for --base.
func TestReviewWithoutADefaultBranch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a")
	run("commit", "-q", "-m", "one")
	useReviewAI(t, &reviewFakeAI{reply: "ok"})
	if code, _, errw := runReviewTest(t, dir, nil); code != 1 || !strings.Contains(errw, "no base branch") {
		t.Errorf("no default base: %d %q", code, errw)
	}
}

// A locked secret store keeps the stored keys shut; with no key in the
// environment the AI says so.
func TestAIExecSetupWithALockedStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	useStore(t, &fakeStore{locked: true}, nil)
	_, err := aiExecSetup(func(string) string { return "" }, "")
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("%v", err)
	}
	_ = secrets.Store(nil)
}

func TestSmallHelpers(t *testing.T) {
	if wfFirstOr("", "d") != "d" || wfFirstOr("x", "d") != "x" {
		t.Fatal("wfFirstOr")
	}
	if got := wfScoreText(score.Result{Score: 0.5, Pass: 0.7, Passed: false}); !strings.Contains(got, "✗") {
		t.Fatal(got)
	}
	if got := wfScoreText(score.Result{Score: 0.9, Pass: 0.7, Passed: true}); !strings.Contains(got, "✓") {
		t.Fatal(got)
	}
	if got := newWebForward("ssh", nil, t.TempDir()).backoff(3); got != 3*time.Second {
		t.Fatalf("backoff %v", got)
	}
	covSessionEnv(t)
	if agentSocketOfCwd() == "" {
		t.Fatal("no socket for the current folder")
	}
	if _, err := laneSpool(errReader{}); err == nil {
		t.Fatal("a broken stream spooled")
	}
}
