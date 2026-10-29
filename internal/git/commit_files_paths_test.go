package git

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
)

// A conflicted file whose name has a space or non-ASCII letters is
// reported by its real name (one path, not split at the space).
func TestCherryPickConflictKeepsSpacedNames(t *testing.T) {
	r := newGitTestRepo(t)
	for _, name := range []string{"my notes.txt", "日本.txt"} {
		r.write(t, name, "base\n")
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-qm", "base")
	r.git(t, "checkout", "-q", "-b", "topic")
	r.write(t, "my notes.txt", "topic\n")
	r.write(t, "日本.txt", "topic\n")
	r.git(t, "commit", "-qam", "topic")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "my notes.txt", "main\n")
	r.write(t, "日本.txt", "main\n")
	r.git(t, "commit", "-qam", "main")

	paths, err := CherryPick(r.dir, "topic")
	if err == nil || !strings.Contains(err.Error(), "2 conflict(s)") {
		t.Fatalf("cherry-pick err = %v, want 2 conflicts", err)
	}
	if strings.Join(paths, "|") != "my notes.txt|日本.txt" {
		t.Errorf("conflicts = %q, want the two real names", paths)
	}
}

func TestCommitFilesBadRevision(t *testing.T) {
	r := committedRepo(t)
	if _, err := CommitFiles(r.dir, "nope"); err == nil {
		t.Error("CommitFiles of a bad rev: want error")
	}
	if _, err := CommitFilesBetween(r.dir, "nope", "HEAD"); err == nil {
		t.Error("CommitFilesBetween with a bad rev: want error")
	}
}

// A rename with spaces, a binary file and a file turned into a symlink,
// as the commit view lists them.
func TestCommitFilesRenameBinaryTypeChange(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "pic.bin", "\x00\x01\x02")
	r.git(t, "add", "pic.bin")
	r.git(t, "commit", "-qm", "bin")
	r.git(t, "mv", "a.txt", "new name.txt")
	r.write(t, "pic.bin", "\x00\x09\x09")
	r.git(t, "add", "pic.bin")
	r.git(t, "rm", "-q", "b.txt")
	r.git(t, "commit", "-qm", "mix")
	info, err := CommitFiles(r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ren := fileOf(t, info, "new name.txt")
	if ren.Status != 'R' || ren.OldPath != "a.txt" {
		t.Errorf("rename = %+v", ren)
	}
	if bin := fileOf(t, info, "pic.bin"); !bin.Binary || bin.Status != 'M' {
		t.Errorf("binary = %+v", bin)
	}
	if d := fileOf(t, info, "b.txt"); d.Status != 'D' || d.Del != 1 {
		t.Errorf("delete = %+v", d)
	}

	r.git(t, "rm", "-q", "pic.bin")
	if err := os.Symlink("new name.txt", r.dir+"/pic.bin"); err != nil {
		t.Fatal(err)
	}
	r.git(t, "add", "pic.bin")
	r.git(t, "commit", "-qm", "link")
	info, err = CommitFiles(r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if f := fileOf(t, info, "pic.bin"); f.Status != 'M' {
		t.Errorf("type change = %+v, want M", f)
	}
}

// Copies read as added files; a path listed twice is kept once; numstat
// rows for unknown paths and broken rows are skipped.
func TestParseRawNumstatOddRecords(t *testing.T) {
	in := ":100644 100644 aaa bbb C75\x00src.go\x00copy.go\x00" +
		":100644 100644 aaa bbb M\x00x.go\x00" +
		":100644 100644 aaa bbb M\x00x.go\x00" +
		":broken\x00" +
		"3\t1\tcopy.go\x00" +
		"7\t7\tother.go\x00" +
		"bad numstat\x00" +
		"2\t0\tx.go\x00"
	files := parseRawNumstat(in)
	if len(files) != 2 {
		t.Fatalf("files = %+v, want copy.go and x.go", files)
	}
	if c := files[0]; c.Path != "copy.go" || c.Status != 'A' || c.OldPath != "" || c.Add != 3 {
		t.Errorf("copy = %+v", c)
	}
	if x := files[1]; x.Path != "x.go" || x.Add != 2 {
		t.Errorf("x.go = %+v", x)
	}
}

func TestCommitMessageAndCompactAge(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a", "a\n")
	r.git(t, "add", "a")
	r.git(t, "commit", "-qm", "subject line", "-m", "body text")
	msg, err := CommitMessage(r.dir, "HEAD")
	if err != nil || msg != "subject line\n\nbody text" {
		t.Errorf("CommitMessage = %q, %v", msg, err)
	}
	if _, err := CommitMessage(r.dir, "nope"); err == nil {
		t.Error("CommitMessage of a bad rev: want error")
	}
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	if got := CompactAge(now.Add(time.Hour).Unix()); got != "now" {
		t.Errorf("CompactAge(future) = %q, want now", got)
	}
	if got := CompactAge(now.Add(-3 * time.Hour).Unix()); got != "3h" {
		t.Errorf("CompactAge(-3h) = %q", got)
	}
}

func TestParseDecorationsDetachedAndRemoteHead(t *testing.T) {
	refs, head := parseDecorations("HEAD, refs/remotes/origin/HEAD, refs/remotes/origin/main, tag: refs/tags/v1")
	if !head {
		t.Error("detached HEAD not seen")
	}
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "v1,origin/main" {
		t.Errorf("refs = %v, want v1 then origin/main (origin/HEAD skipped)", names)
	}
}

// Limit 0 means the default page; a detached HEAD is still marked as HEAD.
func TestCommitsLimitDefault(t *testing.T) {
	r := committedRepo(t)
	cs, more, err := Commits(r.dir, 0, 0)
	if err != nil || more || len(cs) != 1 || cs[0].Subject != "init" {
		t.Errorf("Commits(limit 0) = %+v %v %v", cs, more, err)
	}
	r.git(t, "checkout", "-q", "--detach")
	cs, _, err = Commits(r.dir, 0, 5)
	if err != nil || len(cs) != 1 || !cs[0].IsHead {
		t.Errorf("Commits detached = %+v %v", cs, err)
	}
}
