//go:build unix

package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit puts a shell script named git first on PATH.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCloneRefusesBadInput(t *testing.T) {
	if err := Clone(CloneOptions{URL: "   ", Dest: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "empty repository URL") {
		t.Errorf("Clone with empty URL = %v", err)
	}
	file := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clone(CloneOptions{URL: "https://example.invalid/r.git", Dest: file}); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("Clone onto a file = %v", err)
	}
	if err := CheckCloneDest(t.TempDir()); err != nil {
		t.Errorf("an empty folder is a fine clone target: %v", err)
	}
}

// A clone from a path that does not exist reports git's own reason and is
// not mistaken for a login problem.
func TestCloneMissingSourceSaysWhy(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "c")
	err := Clone(CloneOptions{URL: filepath.Join(t.TempDir(), "nope"), Dest: dest})
	if err == nil || errors.Is(err, ErrCloneAuth) || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Clone of a missing repo = %v", err)
	}
}

// The token reaches git only through the askpass helper (never in argv),
// and the helper file is gone after the clone.
func TestCloneTokenGoesThroughAskpass(t *testing.T) {
	out := filepath.Join(t.TempDir(), "seen")
	fakeGit(t, `
{
  echo "args=$*"
  echo "user=$("$GIT_ASKPASS" 'Username for https://h')"
  echo "pass=$("$GIT_ASKPASS" 'Password for https://h')"
  echo "askpass=$GIT_ASKPASS"
  echo "prompt=$GIT_TERMINAL_PROMPT"
} > `+out+`
echo "Receiving objects: 150% (3/2)" >&2
exit 0
`)
	var got []CloneProgress
	err := Clone(CloneOptions{URL: "https://h/r.git", Dest: filepath.Join(t.TempDir(), "r"), Username: "me", Token: "s3cret",
		OnProgress: func(p CloneProgress) { got = append(got, p) }})
	if err != nil {
		t.Fatal(err)
	}
	seen := readFile(t, out)
	if args := strings.SplitN(seen, "\n", 2)[0]; !strings.HasPrefix(args, "args=clone") || strings.Contains(args, "s3cret") {
		t.Errorf("token is in git's argv: %s", seen)
	}
	for _, want := range []string{"user=me", "pass=s3cret", "prompt=0"} {
		if !strings.Contains(seen, want) {
			t.Errorf("fake git saw %q, want %q", seen, want)
		}
	}
	var askpass string
	for _, l := range strings.Split(seen, "\n") {
		if v, ok := strings.CutPrefix(l, "askpass="); ok {
			askpass = v
		}
	}
	if askpass == "" {
		t.Fatal("no GIT_ASKPASS given to git")
	}
	if _, err := os.Stat(askpass); !os.IsNotExist(err) {
		t.Errorf("askpass helper %s still there after the clone", askpass)
	}
	if len(got) != 1 || got[0].Percent != 100 {
		t.Errorf("progress = %+v, want one update capped at 100%%", got)
	}
}

func TestCloneAuthFailure(t *testing.T) {
	fakeGit(t, `echo "fatal: Authentication failed for 'https://h/r.git/'" >&2; exit 128`)
	err := Clone(CloneOptions{URL: "https://h/r.git", Dest: filepath.Join(t.TempDir(), "r")})
	if !errors.Is(err, ErrCloneAuth) {
		t.Errorf("Clone with auth failure = %v, want ErrCloneAuth", err)
	}
}

// Only git's last lines are kept in the error, and a silent failure still
// returns the exit error.
func TestCloneErrorKeepsLastLines(t *testing.T) {
	fakeGit(t, `for i in 1 2 3 4 5 6 7 8 9 10 11 12; do echo "line $i" >&2; done; exit 1`)
	err := Clone(CloneOptions{URL: "https://h/r.git", Dest: filepath.Join(t.TempDir(), "r")})
	if err == nil || strings.Contains(err.Error(), "line 4\n") || !strings.Contains(err.Error(), "line 5") || !strings.Contains(err.Error(), "line 12") {
		t.Errorf("Clone error = %v, want only lines 5..12", err)
	}
	fakeGit(t, `exit 3`)
	err = Clone(CloneOptions{URL: "https://h/r.git", Dest: filepath.Join(t.TempDir(), "r")})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("silent Clone failure = %v, want the exit status", err)
	}
}

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for in, want := range map[string]string{
		"~":        home,
		"~/src/x":  filepath.Join(home, "src", "x"),
		"~other/x": "~other/x",
		"/abs":     "/abs",
	} {
		if got := ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
	if got := DefaultCloneDest("/w", false, ""); got != filepath.Join("/w", "repo") {
		t.Errorf("DefaultCloneDest with no name = %q", got)
	}
}
