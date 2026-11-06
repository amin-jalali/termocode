package git

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// commitAt commits all changes as author at the given date (author and
// committer date both), so date filters are exact.
func commitAt(t *testing.T, r *gitTestRepo, author, date, msg string) {
	t.Helper()
	r.git(t, "add", "-A")
	cmd := exec.Command("git", "commit", "-q", "--author", author+" <"+strings.ToLower(author)+"@example.com>", "-m", msg)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
}

func searchRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	commitAt(t, r, "Ann", "2026-01-01T10:00:00", "feat: add a")
	r.write(t, "docs/b.md", "needle here\n")
	commitAt(t, r, "Bob", "2026-02-01T10:00:00", "docs: add b")
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "c.txt", "side\n")
	commitAt(t, r, "Ann", "2026-03-01T10:00:00", "fix: side work")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "a.txt", "one\ntwo\n")
	commitAt(t, r, "Bob", "2026-04-01T10:00:00", "Fix: a again")
	return r
}

func logSubjects(es []LogEntry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.Subject)
	}
	return strings.Join(s, "|")
}

// GITX-9 (E11–E17, E25): the log is searched by message (any case, plain
// text), author, dates, a path or folder, a branch, all branches and the
// changes (pickaxe).
func TestSearchLogFilters(t *testing.T) {
	r := searchRepo(t)
	for _, c := range []struct {
		name string
		f    LogFilter
		want string
	}{
		{"none", LogFilter{}, "Fix: a again|docs: add b|feat: add a"},
		{"text any case", LogFilter{Text: "fix"}, "Fix: a again"},
		{"text is plain, not a regexp", LogFilter{Text: "a."}, ""},
		{"author", LogFilter{Author: "bob"}, "Fix: a again|docs: add b"},
		{"since", LogFilter{Since: "2026-01-15"}, "Fix: a again|docs: add b"},
		{"until", LogFilter{Until: "2026-01-15"}, "feat: add a"},
		{"folder", LogFilter{Path: "docs"}, "docs: add b"},
		{"file", LogFilter{Path: "a.txt"}, "Fix: a again|feat: add a"},
		{"branch", LogFilter{Rev: "side"}, "fix: side work|docs: add b|feat: add a"},
		{"all branches", LogFilter{All: true, Text: "fix"}, "Fix: a again|fix: side work"},
		{"changes", LogFilter{Change: "needle"}, "docs: add b"},
		{"text and author", LogFilter{Text: "add", Author: "Ann"}, "feat: add a"},
	} {
		got, err := SearchLog(r.dir, c.f, 50)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if s := logSubjects(got); s != c.want {
			t.Errorf("%s: got %q, want %q", c.name, s, c.want)
		}
	}
	if got, _ := SearchLog(r.dir, LogFilter{}, 2); len(got) != 2 {
		t.Errorf("limit: %d entries", len(got))
	}
}

// A branch or path that looks like an option is refused, never passed to
// git as one.
func TestSearchLogRefusesOptions(t *testing.T) {
	r := searchRepo(t)
	if _, err := SearchLog(r.dir, LogFilter{Rev: "--output=/tmp/x"}, 10); err == nil {
		t.Error("an option as branch was accepted")
	}
	if _, err := SearchLog(r.dir, LogFilter{Rev: "nope"}, 10); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("unknown branch: %v", err)
	}
	if got, err := SearchLog(r.dir, LogFilter{Path: "-x"}, 10); err != nil || len(got) != 0 {
		t.Errorf("a path starting with - is a path: %v %v", got, err)
	}
}

// The one-line query of Search Log…: words are message text; key:value
// sets a filter; quotes keep spaces.
func TestParseLogQuery(t *testing.T) {
	f := ParseLogQuery(`fix crash author:"Ann Lee" since:2.weeks until:2026-01-01 path:internal/git branch:main all changes:needle`)
	want := LogFilter{Text: "fix crash", Author: "Ann Lee", Since: "2.weeks", Until: "2026-01-01", Path: "internal/git", Rev: "main", All: true, Change: "needle"}
	if f != want {
		t.Errorf("got %+v\nwant %+v", f, want)
	}
	if f := ParseLogQuery("  "); f != (LogFilter{}) {
		t.Errorf("empty: %+v", f)
	}
	if f := ParseLogQuery("http://x a:b"); f.Text != "http://x a:b" {
		t.Errorf("unknown keys stay text: %+v", f)
	}
	if s := (LogFilter{Text: "x", Author: "Ann", All: true}).String(); s != `"x" author:Ann all branches` {
		t.Errorf("String: %q", s)
	}
}

// GITX-9 (E16): Go to Commit resolves a hash, a branch or a tag to its
// commit; anything else (or an option) is an error.
func TestResolveCommit(t *testing.T) {
	r := searchRepo(t)
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	for _, ref := range []string{head[:7], "main", "HEAD"} {
		full, err := ResolveCommit(r.dir, ref)
		if err != nil || full != head {
			t.Errorf("%s: %q %v", ref, full, err)
		}
	}
	for _, bad := range []string{"", "nope", "--all", "a.txt"} {
		if _, err := ResolveCommit(r.dir, bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
}
