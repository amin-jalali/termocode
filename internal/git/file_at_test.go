package git

import (
	"strings"
	"testing"
)

// FileAt reads one file as git keeps it: in the index, at HEAD or at a
// commit (also its parent, "hash^"). A file that is not there is not
// found (no error); a long file stops at max bytes and says so; the
// user's text filters never run.
func TestFileAtRevisions(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "two\n")
	r.git(t, "commit", "-q", "-am", "second")
	r.write(t, "a.txt", "three\n")
	r.git(t, "add", "a.txt")
	r.write(t, "a.txt", "four\n")
	head := strings.TrimSpace(r.git(t, "rev-parse", "--short", "HEAD"))

	for rev, want := range map[string]string{"index": "three\n", "HEAD": "two\n", head: "two\n", head + "^": "one\n"} {
		got, found, cut, err := FileAt(r.dir, rev, "a.txt", 1<<20)
		if err != nil || !found || cut || got != want {
			t.Errorf("%s: %q found=%v cut=%v err=%v, want %q", rev, got, found, cut, err, want)
		}
	}
	if _, found, _, err := FileAt(r.dir, "HEAD", "new.txt", 1<<20); err != nil || found {
		t.Errorf("a file not in HEAD: found=%v err=%v", found, err)
	}
	if _, found, _, err := FileAt(r.dir, head+"^^", "a.txt", 1<<20); err != nil || found {
		t.Errorf("before the first commit: found=%v err=%v", found, err)
	}
	r.write(t, "big.txt", strings.Repeat("a line of text\n", 2000))
	r.git(t, "add", "big.txt")
	big, found, cut, err := FileAt(r.dir, "index", "big.txt", 4096)
	if err != nil || !found || !cut || len(big) > 4096 {
		t.Errorf("capped: %d bytes found=%v cut=%v err=%v", len(big), found, cut, err)
	}
	if _, _, _, err := FileAt(r.dir, "--output=x", "a.txt", 10); err == nil {
		t.Error("a revision that looks like an option is read")
	}
}
