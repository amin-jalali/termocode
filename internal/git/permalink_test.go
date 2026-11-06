package git

import (
	"strings"
	"testing"
)

// GITX-29 (O24): WebBase turns a remote URL into its web page (ssh, scp
// style, https; user, token and .git dropped).
func TestWebBase(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:o/r.git":                      "https://github.com/o/r",
		"https://github.com/o/r.git":                  "https://github.com/o/r",
		"https://user:" + "tok" + "en@github.com/o/r": "https://github.com/o/r",
		"ssh://git@github.com/o/r.git":                "https://github.com/o/r",
		"ssh://git@gitlab.com:22/g/sub/r.git":         "https://gitlab.com/g/sub/r",
		"git@gitlab.example.com:g/r":                  "https://gitlab.example.com/g/r",
	} {
		if got, ok := WebBase(in); !ok || got != want {
			t.Errorf("WebBase(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "/srv/repo.git", "file:///srv/r", "../r"} {
		if got, ok := WebBase(bad); ok {
			t.Errorf("WebBase(%q) = %q", bad, got)
		}
	}
}

// LineLink and CommitLink use GitHub's form, and GitLab's on a GitLab
// host.
func TestLineAndCommitLinks(t *testing.T) {
	if got := LineLink("https://github.com/o/r", "abc", "dir/a b.go", 3, 5); got != "https://github.com/o/r/blob/abc/dir/a%20b.go#L3-L5" {
		t.Error(got)
	}
	if got := LineLink("https://github.com/o/r", "abc", "a.go", 7, 7); got != "https://github.com/o/r/blob/abc/a.go#L7" {
		t.Error(got)
	}
	if got := LineLink("https://gitlab.com/g/r", "abc", "a.go", 3, 5); got != "https://gitlab.com/g/r/-/blob/abc/a.go#L3-5" {
		t.Error(got)
	}
	if got := CommitLink("https://gitlab.com/g/r", "abc"); got != "https://gitlab.com/g/r/-/commit/abc" {
		t.Error(got)
	}
	if got := CommitLink("https://github.com/o/r", "abc"); got != "https://github.com/o/r/commit/abc" {
		t.Error(got)
	}
}

// RemoteWeb reads the web page of origin (or the only remote) and
// OnRemote says whether a commit is on a remote branch.
func TestRemoteWebAndOnRemote(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-qm", "a")
	if _, err := RemoteWeb(r.dir); err == nil {
		t.Fatal("no remote: want an error")
	}
	r.git(t, "remote", "add", "origin", "git@github.com:o/r.git")
	if got, err := RemoteWeb(r.dir); err != nil || got != "https://github.com/o/r" {
		t.Fatalf("%q %v", got, err)
	}
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	if OnRemote(r.dir, head) {
		t.Error("an unpushed commit is on the remote")
	}
	r.git(t, "update-ref", "refs/remotes/origin/main", head)
	if !OnRemote(r.dir, head) {
		t.Error("a pushed commit is not on the remote")
	}
}
