package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DiffFile shows one side of a file's change: the staged diff, the
// unstaged one, or a new file as all added; the output stops at max
// bytes (cut at a line end) and says so.
func TestDiffFileSidesAndCap(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\ntwo\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "a.txt", "one\nTWO\n")
	r.git(t, "add", "a.txt")
	r.write(t, "a.txt", "ONE\nTWO\n")
	r.write(t, "new.txt", "fresh\n")

	staged, cut, err := DiffFile(r.dir, "a.txt", true, false, 1<<20)
	if err != nil || cut || !strings.Contains(staged, "+TWO") || strings.Contains(staged, "+ONE") {
		t.Fatalf("staged diff %q cut=%v err=%v", staged, cut, err)
	}
	work, _, err := DiffFile(r.dir, "a.txt", false, false, 1<<20)
	if err != nil || !strings.Contains(work, "+ONE") || strings.Contains(work, "+TWO") {
		t.Fatalf("unstaged diff %q err=%v", work, err)
	}
	added, _, err := DiffFile(r.dir, "new.txt", false, true, 1<<20)
	if err != nil || !strings.Contains(added, "+fresh") || !strings.Contains(added, "@@ -0,0 +1 @@") {
		t.Fatalf("untracked diff %q err=%v", added, err)
	}

	r.write(t, "big.txt", strings.Repeat("a line of text\n", 2000))
	big, cut, err := DiffFile(r.dir, "big.txt", false, true, 4096)
	if err != nil || !cut || len(big) > 4096 || !strings.HasSuffix(big, "\n") {
		t.Fatalf("capped diff: %d bytes cut=%v err=%v", len(big), cut, err)
	}
}

// An untracked symlink is diffed as a link (its target name), never by
// reading the file it points at, which may be outside the workspace.
func TestDiffFileSymlinkIsTheLink(t *testing.T) {
	r := newGitTestRepo(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("the outside text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.dir, "link")); err != nil {
		t.Skip("no symlinks here")
	}
	out, _, err := DiffFile(r.dir, "link", false, true, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "the outside text") || !strings.Contains(out, "secret.txt") {
		t.Fatalf("symlink diff read the target:\n%s", out)
	}
}

// A user's external diff program or text filter never runs.
func TestDiffFileNoExternalDiff(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	mark := filepath.Join(t.TempDir(), "ran")
	r.git(t, "config", "diff.external", "sh -c 'touch "+mark+"'")
	r.write(t, "a.txt", "two\n")
	if _, _, err := DiffFile(r.dir, "a.txt", false, false, 1<<20); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("the external diff program ran")
	}
}
