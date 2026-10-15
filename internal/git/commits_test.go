package git

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// commitN makes one commit that changes f.txt.
func (r *gitTestRepo) commitN(t *testing.T, msg string) {
	t.Helper()
	r.write(t, "f.txt", msg+"\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", msg)
}

func TestCommitsParse(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "init")
	r.git(t, "tag", "v1.0")
	r.git(t, "checkout", "-q", "-b", "feature/x")
	r.commitN(t, `feat: a "quoted" | piped subject`)
	r.git(t, "checkout", "-q", "main")
	r.write(t, "g.txt", "g\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "on main")
	r.git(t, "merge", "-q", "--no-ff", "-m", "Merge feature/x", "feature/x")
	// A remote-tracking ref on the first commit, as after a fetch.
	r.git(t, "update-ref", "refs/remotes/origin/main", "v1.0")

	cs, more, err := Commits(r.dir, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if more || len(cs) != 4 {
		t.Fatalf("got %d commits (more=%v), want 4", len(cs), more)
	}
	head := cs[0]
	if !head.IsHead || !head.IsMerge || len(head.Parents) != 2 || head.Subject != "Merge feature/x" {
		t.Errorf("head = %+v", head)
	}
	if len(head.Refs) != 1 || head.Refs[0] != (Ref{Name: "main", Kind: RefLocal}) {
		t.Errorf("head refs = %+v (HEAD -> must be dropped)", head.Refs)
	}
	if head.Author != "Test" || head.Committed == 0 || len(head.FullHash) != 40 || !strings.HasPrefix(head.FullHash, head.Hash) {
		t.Errorf("head fields = %+v", head)
	}
	var feat, first GraphCommit
	for _, c := range cs {
		switch {
		case strings.HasPrefix(c.Subject, "feat:"):
			feat = c
		case c.Subject == "init":
			first = c
		}
	}
	if feat.Subject != `feat: a "quoted" | piped subject` || len(feat.Refs) != 1 || feat.Refs[0] != (Ref{Name: "feature/x", Kind: RefLocal}) {
		t.Errorf("feature commit = %+v", feat)
	}
	want := []Ref{{Name: "v1.0", Kind: RefTag}, {Name: "origin/main", Kind: RefRemote}}
	if len(first.Refs) != 2 || first.Refs[0] != want[0] || first.Refs[1] != want[1] {
		t.Errorf("first refs = %+v, want %+v", first.Refs, want)
	}
	if first.IsMerge || first.IsHead {
		t.Errorf("first = %+v", first)
	}
}

func TestCommitsPaging(t *testing.T) {
	r := newGitTestRepo(t)
	for _, m := range []string{"c1", "c2", "c3", "c4", "c5"} {
		r.commitN(t, m)
	}
	cs, more, err := Commits(r.dir, 0, 2)
	if err != nil || len(cs) != 2 || !more || cs[0].Subject != "c5" {
		t.Fatalf("page 1: %d commits, more=%v, err=%v", len(cs), more, err)
	}
	cs, more, err = Commits(r.dir, 4, 2)
	if err != nil || len(cs) != 1 || more || cs[0].Subject != "c1" {
		t.Fatalf("last page: %+v, more=%v, err=%v", cs, more, err)
	}
}

func TestCommitsUnpushed(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "pushed")
	r.git(t, "remote", "add", "origin", "https://example.invalid/x.git")
	r.git(t, "update-ref", "refs/remotes/origin/main", "HEAD")
	r.git(t, "config", "branch.main.remote", "origin")
	r.git(t, "config", "branch.main.merge", "refs/heads/main")
	r.commitN(t, "local only")
	cs, _, err := Commits(r.dir, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !cs[0].Unpushed || cs[1].Unpushed {
		t.Errorf("unpushed = %v, %v; want true, false", cs[0].Unpushed, cs[1].Unpushed)
	}
}

func TestCommitsNoUpstreamIsNotError(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "only")
	cs, _, err := Commits(r.dir, 0, 10)
	if err != nil || len(cs) != 1 || cs[0].Unpushed {
		t.Fatalf("no upstream: %+v, err=%v", cs, err)
	}
}

func TestCommitsEmptyRepo(t *testing.T) {
	r := newGitTestRepo(t)
	cs, more, err := Commits(r.dir, 0, 10)
	if err != nil || len(cs) != 0 || more {
		t.Fatalf("empty repo: %+v, more=%v, err=%v", cs, more, err)
	}
}

func TestGroupLabel(t *testing.T) {
	now := time.Date(2026, 10, 15, 14, 0, 0, 0, time.Local) // a Thursday
	defer clock.Set(func() time.Time { return now })()
	at := func(y int, mo time.Month, d, h int) int64 { return time.Date(y, mo, d, h, 0, 0, 0, time.Local).Unix() }
	cases := []struct {
		epoch int64
		want  string
	}{
		{at(2026, 10, 15, 1), "Today"},
		{at(2026, 10, 14, 23), "Yesterday"},
		{at(2026, 10, 9, 12), "This week"},
		{at(2026, 10, 8, 12), "October"},
		{at(2026, 9, 30, 12), "September"},
		{at(2025, 12, 31, 12), "December 2025"},
		{at(2026, 10, 16, 12), "Today"}, // clock skew: a commit "from the future"
	}
	for _, c := range cases {
		if got := GroupLabel(c.epoch); got != c.want {
			t.Errorf("GroupLabel(%s) = %q, want %q", time.Unix(c.epoch, 0), got, c.want)
		}
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Amin Jalali":       "AJ",
		"amin van der berg": "AB",
		"amin":              "AM",
		"x":                 "X",
		"李小龙":               "李",
		"":                  "??",
		"  ":                "??",
	} {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}
