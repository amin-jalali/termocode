package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCloneProgress(t *testing.T) {
	cases := []struct {
		in    string
		phase string
		pct   int
		ok    bool
	}{
		{"Receiving objects:  45% (450/1000), 1.20 MiB | 2.00 MiB/s", "Receiving objects", 45, true},
		{"remote: Counting objects: 100% (12/12), done.", "Counting objects", 100, true},
		{"Resolving deltas:   7% (1/14)", "Resolving deltas", 7, true},
		{"Cloning into 'repo'...", "Cloning", -1, true},
		{"fatal: repository not found", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		p, ok := ParseCloneProgress(c.in)
		if ok != c.ok || (ok && (p.Phase != c.phase || p.Percent != c.pct)) {
			t.Errorf("%q → %+v %v", c.in, p, ok)
		}
	}
}

func TestIsAuthFailure(t *testing.T) {
	if !IsAuthFailure("fatal: could not read Username for 'https://github.com': terminal prompts disabled") {
		t.Error("terminal prompt message should count as auth failure")
	}
	if !IsAuthFailure("remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://x'") {
		t.Error("gitlab message should count")
	}
	if IsAuthFailure("fatal: destination path 'x' already exists") {
		t.Error("not an auth failure")
	}
}

func TestRepoNameAndHost(t *testing.T) {
	cases := []struct{ url, name, host string }{
		{"https://github.com/owner/repo.git", "repo", "github.com"},
		{"https://github.com/owner/repo/", "repo", "github.com"},
		{"https://user@gitlab.com:8443/group/sub/proj", "proj", "gitlab.com"},
		{"git@github.com:owner/repo.git", "repo", "github.com"},
		{"ssh://git@host.xz/path/to/r.git", "r", "host.xz"},
		{"/tmp/local/thing.git", "thing", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := RepoNameFromURL(c.url); got != c.name {
			t.Errorf("name(%q) = %q, want %q", c.url, got, c.name)
		}
		if got := HostFromURL(c.url); got != c.host {
			t.Errorf("host(%q) = %q, want %q", c.url, got, c.host)
		}
	}
	if !IsHTTPURL("HTTPS://x/y") || IsHTTPURL("git@x:y") {
		t.Error("IsHTTPURL mismatch")
	}
}

func TestDefaultCloneDestAndSplitToken(t *testing.T) {
	if got := DefaultCloneDest("/w/proj", true, "r"); got != "/w/r" {
		t.Errorf("repo cwd → sibling, got %q", got)
	}
	if got := DefaultCloneDest("/home/me", false, "r"); got != "/home/me/r" {
		t.Errorf("plain cwd → child, got %q", got)
	}
	if u, tok := SplitUserToken("ghp_abc"); u != "" || tok != "ghp_abc" {
		t.Errorf("bare token: %q %q", u, tok)
	}
	if u, tok := SplitUserToken("alice:s3cr:et"); u != "alice" || tok != "s3cr:et" {
		t.Errorf("user:token: %q %q", u, tok)
	}
}

func TestCloneEnv(t *testing.T) {
	base := []string{"PATH=/bin", "GIT_ASKPASS=/usr/bin/ksshaskpass", "GIT_TERMINAL_PROMPT=1"}
	env := strings.Join(cloneEnv(base, "", "", ""), "\n")
	if !strings.Contains(env, "GIT_TERMINAL_PROMPT=0") || strings.Contains(env, "GIT_TERMINAL_PROMPT=1") {
		t.Errorf("prompt not disabled: %s", env)
	}
	if !strings.Contains(env, "GIT_ASKPASS=/usr/bin/ksshaskpass") {
		t.Error("user askpass must be kept when no token")
	}
	env = strings.Join(cloneEnv(base, "/tmp/a.sh", "", "tok"), "\n")
	if strings.Contains(env, "ksshaskpass") || !strings.Contains(env, "GIT_ASKPASS=/tmp/a.sh") ||
		!strings.Contains(env, "TERMOCODE_GIT_TOKEN=tok") || !strings.Contains(env, "TERMOCODE_GIT_USER=x-access-token") {
		t.Errorf("token env wrong: %s", env)
	}
}

func TestAskpassScript(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	p := filepath.Join(t.TempDir(), "askpass.sh")
	if err := os.WriteFile(p, []byte(askpassScript), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(prompt string) string {
		cmd := exec.Command(p, prompt)
		cmd.Env = []string{"TERMOCODE_GIT_USER=bob", "TERMOCODE_GIT_TOKEN=s3cret"}
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run("Username for 'https://github.com': "); got != "bob" {
		t.Errorf("username → %q", got)
	}
	if got := run("Password for 'https://bob@github.com': "); got != "s3cret" {
		t.Errorf("password → %q", got)
	}
}

func TestCloneLocalRepo(t *testing.T) {
	src := newGitTestRepo(t)
	src.write(t, "a.txt", "hi\n")
	src.git(t, "add", ".")
	src.git(t, "commit", "-q", "-m", "init")

	dest := filepath.Join(t.TempDir(), "sub", "clone")
	var phases int
	err := Clone(CloneOptions{URL: src.dir, Dest: dest, OnProgress: func(CloneProgress) { phases++ }})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "a.txt")); err != nil {
		t.Fatalf("cloned file missing: %v", err)
	}
	// Cloning again into the now non-empty folder is refused up front.
	if err := Clone(CloneOptions{URL: src.dir, Dest: dest}); err == nil || errors.Is(err, ErrCloneAuth) {
		t.Errorf("want non-auth error for non-empty dest, got %v", err)
	}
}
