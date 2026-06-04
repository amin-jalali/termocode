package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitTestRepo is a freshly-initialised git repository at a temp directory,
// pre-configured with a user name/email so commits don't fail under CI.
// The cleanup func is registered via t.Cleanup so individual tests don't
// have to remember it.
type gitTestRepo struct {
	dir string
}

func newGitTestRepo(t *testing.T) *gitTestRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available in PATH; skipping git tests")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		// Disable signing & gpg prompts in case the host config requests them.
		{"config", "commit.gpgsign", "false"},
		{"config", "tag.gpgsign", "false"},
		// Pin the initial branch so default-branch differences (master vs
		// main) don't make tests flaky.
		{"checkout", "-b", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	return &gitTestRepo{dir: dir}
}

func (r *gitTestRepo) write(t *testing.T, name, body string) string {
	t.Helper()
	full := filepath.Join(r.dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func (r *gitTestRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestIsRepo(t *testing.T) {
	r := newGitTestRepo(t)
	if !IsRepo(r.dir) {
		t.Errorf("IsRepo(%q) = false, want true", r.dir)
	}
	if IsRepo(t.TempDir()) {
		t.Errorf("IsRepo on plain tempdir = true, want false")
	}
}

func TestStageAndStatus(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "new.txt", "hello\n")

	files, err := Status(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].Untracked() || files[0].Path != "new.txt" {
		t.Fatalf("Status before stage = %#v, want one untracked new.txt", files)
	}

	if err := Stage(r.dir, "new.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ = Status(r.dir)
	if len(files) != 1 || !files[0].Staged() || files[0].Path != "new.txt" {
		t.Fatalf("Status after stage = %#v, want one staged new.txt", files)
	}
}

func TestUnstage(t *testing.T) {
	r := newGitTestRepo(t)
	// Need an initial commit so `git restore --staged` has something to
	// restore *to*. Otherwise "no commits yet" makes restore a no-op.
	r.write(t, "init.txt", "one\n")
	r.git(t, "add", "init.txt")
	r.git(t, "commit", "-q", "-m", "init")

	r.write(t, "init.txt", "two\n")
	if err := Stage(r.dir, "init.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Unstage(r.dir, "init.txt"); err != nil {
		t.Fatal(err)
	}
	files, _ := Status(r.dir)
	if len(files) != 1 || !files[0].Unstaged() || files[0].Staged() {
		t.Fatalf("after Unstage = %#v, want unstaged-only", files)
	}
}

func TestCommit(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	if err := Stage(r.dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := Commit(r.dir, "first commit"); err != nil {
		t.Fatal(err)
	}
	out := r.git(t, "log", "--oneline")
	if !strings.Contains(out, "first commit") {
		t.Errorf("log missing commit: %q", out)
	}
}

func TestCommitEmptyMessageRejected(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	_ = Stage(r.dir, "a.txt")
	if err := Commit(r.dir, "   "); err == nil {
		t.Errorf("Commit with whitespace-only message should fail")
	}
}

func TestDiscard_Tracked(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")

	r.write(t, "a.txt", "two\n")
	if err := Discard(r.dir, "a.txt"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(r.dir, "a.txt"))
	if string(body) != "one\n" {
		t.Errorf("Discard didn't restore: got %q", body)
	}
}

func TestDiscard_Untracked(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "junk.tmp", "garbage\n")
	if err := Discard(r.dir, "junk.tmp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "junk.tmp")); !os.IsNotExist(err) {
		t.Errorf("Discard on untracked should remove file; got %v", err)
	}
}

func TestBranches(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	if err := CreateBranch(r.dir, "feature"); err != nil {
		t.Fatal(err)
	}
	branches, err := Branches(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	gotMain, gotFeature := false, false
	for _, b := range branches {
		if b == "main" {
			gotMain = true
		}
		if b == "feature" {
			gotFeature = true
		}
	}
	if !gotMain || !gotFeature {
		t.Errorf("Branches = %v, want main and feature", branches)
	}
}

func TestCheckout(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	if err := CreateBranch(r.dir, "feat"); err != nil {
		t.Fatal(err)
	}
	if err := Checkout(r.dir, "main"); err != nil {
		t.Fatal(err)
	}
	br, _ := GetBranch(r.dir)
	if br.Name != "main" {
		t.Errorf("after Checkout(main), branch = %q, want main", br.Name)
	}
}

func TestStashAndPop(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "a.txt", "two\n")

	if err := Stash(r.dir, "wip"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(r.dir, "a.txt"))
	if string(body) != "one\n" {
		t.Fatalf("after Stash, working tree = %q, want one\\n", body)
	}
	stashes, err := StashList(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stashes) != 1 || !strings.Contains(stashes[0].Message, "wip") {
		t.Errorf("StashList = %#v, want one entry with 'wip'", stashes)
	}
	if err := StashPop(r.dir); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(filepath.Join(r.dir, "a.txt"))
	if string(body) != "two\n" {
		t.Errorf("after Pop, working tree = %q, want two\\n", body)
	}
}

func TestStash_NoChanges(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	if err := Stash(r.dir, ""); err == nil {
		t.Errorf("Stash on clean tree should report 'no changes'")
	}
}

func TestDiff_Tracked(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "one\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.write(t, "a.txt", "two\n")
	out, err := Diff(r.dir, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+two") || !strings.Contains(out, "-one") {
		t.Errorf("Diff missing expected hunk: %q", out)
	}
}

func TestDiff_Untracked(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "new.txt", "added\n")
	out, err := Diff(r.dir, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+added") || !strings.Contains(out, "new file") {
		t.Errorf("synthetic untracked diff missing expected lines: %q", out)
	}
}

func TestLog(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "v1\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")
	r.write(t, "a.txt", "v2\n")
	r.git(t, "commit", "-q", "-am", "second")

	entries, err := Log(r.dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("Log returned %d entries, want 2", len(entries))
	}
	// Newest first.
	if !strings.Contains(entries[0].Subject, "second") {
		t.Errorf("Log[0] subject = %q, want 'second'", entries[0].Subject)
	}
	if entries[0].Author == "" || entries[0].When == "" {
		t.Errorf("Log[0] missing author/when: %#v", entries[0])
	}
}

func TestGraphLog(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "v1\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "first")
	// Branch off, commit, then merge back with a merge commit so the graph
	// has real topology (a connector line) to parse.
	r.git(t, "checkout", "-q", "-b", "feature")
	r.write(t, "b.txt", "feat\n")
	r.git(t, "add", "b.txt")
	r.git(t, "commit", "-q", "-m", "feature work")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "c.txt", "main\n")
	r.git(t, "add", "c.txt")
	r.git(t, "commit", "-q", "-m", "main work")
	r.git(t, "merge", "-q", "--no-ff", "feature", "-m", "merge feature")

	lines, err := GraphLog(r.dir, 50)
	if err != nil {
		t.Fatal(err)
	}

	var commits, connectors int
	var sawMerge bool
	for _, l := range lines {
		if l.Hash == "" {
			connectors++
			continue
		}
		commits++
		// Every commit node's art must contain the node marker.
		if !strings.Contains(l.Art, "*") {
			t.Errorf("commit line art %q missing '*' node marker (hash %q)", l.Art, l.Hash)
		}
		// Hash must be hex, not leak any art characters.
		if strings.ContainsAny(l.Hash, " *|/\\") {
			t.Errorf("hash %q leaked graph art", l.Hash)
		}
		if l.Subject == "merge feature" {
			sawMerge = true
		}
	}
	if commits != 4 {
		t.Errorf("commits = %d, want 4", commits)
	}
	if connectors == 0 {
		t.Errorf("expected at least one connector line from the merge topology")
	}
	if !sawMerge {
		t.Errorf("merge commit subject not parsed")
	}

	// The newest commit is the merge; it must be flagged IsMerge + IsHead and
	// carry a compact age.
	var head *GraphLine
	for i := range lines {
		if lines[i].Hash != "" {
			head = &lines[i]
			break
		}
	}
	if head == nil {
		t.Fatal("no commit node found")
	}
	if !head.IsHead {
		t.Errorf("newest commit should be flagged IsHead: %+v", head)
	}
	if !head.IsMerge {
		t.Errorf("newest commit is a merge, want IsMerge: %+v", head)
	}
	if head.Age == "" {
		t.Errorf("newest commit missing compact age: %+v", head)
	}
}

func TestCompactAge(t *testing.T) {
	cases := map[string]string{
		"just now":       "now",
		"3 seconds ago":  "now",
		"5 minutes ago":  "5m",
		"2 hours ago":    "2h",
		"1 hour ago":     "1h",
		"4 days ago":     "4d",
		"2 weeks ago":    "2w",
		"3 months ago":   "3mo",
		"1 year ago":     "1y",
		"":               "now",
	}
	for in, want := range cases {
		if got := compactAge(in); got != want {
			t.Errorf("compactAge(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileHistory(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "1\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "add a")
	r.write(t, "b.txt", "1\n")
	r.git(t, "add", "b.txt")
	r.git(t, "commit", "-q", "-m", "add b")
	r.write(t, "a.txt", "2\n")
	r.git(t, "commit", "-q", "-am", "edit a")

	entries, err := FileHistory(r.dir, "a.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	// FileHistory("a.txt") should return only commits that touched a.txt:
	// "add a" and "edit a", but NOT "add b".
	if len(entries) != 2 {
		t.Fatalf("FileHistory returned %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		if strings.Contains(e.Subject, "add b") {
			t.Errorf("FileHistory included unrelated commit: %#v", e)
		}
	}
}

func TestBlameLineAt(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "x.txt", "alpha\nbeta\ngamma\n")
	r.git(t, "add", "x.txt")
	r.git(t, "commit", "-q", "-m", "initial three lines")

	bl, err := BlameLineAt(r.dir, "x.txt", 2)
	if err != nil {
		t.Fatal(err)
	}
	if bl.Hash == "" || bl.Hash == "uncommitted" {
		t.Errorf("BlameLineAt should report a hash for a committed line: %#v", bl)
	}
	if bl.Author != "Test" {
		t.Errorf("BlameLineAt author = %q, want Test", bl.Author)
	}
	if !strings.Contains(bl.Subject, "initial three lines") {
		t.Errorf("BlameLineAt subject = %q, want 'initial three lines'", bl.Subject)
	}
}

func TestBlameLineAt_Uncommitted(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "x.txt", "one\n")
	r.git(t, "add", "x.txt")
	r.git(t, "commit", "-q", "-m", "init")
	// Modify without committing — line should now be uncommitted.
	r.write(t, "x.txt", "modified\n")
	bl, err := BlameLineAt(r.dir, "x.txt", 1)
	if err != nil {
		t.Fatal(err)
	}
	if bl.Hash != "uncommitted" || bl.Author != "you" {
		t.Errorf("Uncommitted line should be tagged 'you / uncommitted', got %#v", bl)
	}
}

func TestTags(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "x\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-q", "-m", "init")
	r.git(t, "tag", "v0.1.0")
	r.git(t, "tag", "v0.2.0")
	tags, err := Tags(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	// Both tags should be present. We don't assert order: when both tags
	// point to the same commit they share a creator-date and `--sort` falls
	// back to alphabetical, which means the strict "newest first" guarantee
	// only holds when tags target distinct commits.
	if len(tags) != 2 {
		t.Fatalf("Tags returned %d, want 2: %v", len(tags), tags)
	}
	gotV1, gotV2 := false, false
	for _, tag := range tags {
		if tag == "v0.1.0" {
			gotV1 = true
		}
		if tag == "v0.2.0" {
			gotV2 = true
		}
	}
	if !gotV1 || !gotV2 {
		t.Errorf("Tags = %v, want both v0.1.0 and v0.2.0", tags)
	}
}

func TestDiffAgainst(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "f.txt", "v1\n")
	r.git(t, "add", "f.txt")
	r.git(t, "commit", "-q", "-m", "v1")
	r.write(t, "f.txt", "v2\n")
	r.git(t, "commit", "-q", "-am", "v2")

	out, err := DiffAgainst(r.dir, "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "+v2") || !strings.Contains(out, "-v1") {
		t.Errorf("DiffAgainst missing expected hunk: %q", out)
	}
}

func TestFileStatusFlags(t *testing.T) {
	cases := []struct {
		code              string
		staged, unstaged  bool
		untracked         bool
	}{
		{"M ", true, false, false},
		{" M", false, true, false},
		{"MM", true, true, false},
		{"A ", true, false, false},
		{"??", false, false, true},
		{"  ", false, false, false},
	}
	for _, c := range cases {
		f := FileStatus{Code: c.code}
		if got := f.Staged(); got != c.staged {
			t.Errorf("Staged(%q) = %v, want %v", c.code, got, c.staged)
		}
		if got := f.Unstaged(); got != c.unstaged {
			t.Errorf("Unstaged(%q) = %v, want %v", c.code, got, c.unstaged)
		}
		if got := f.Untracked(); got != c.untracked {
			t.Errorf("Untracked(%q) = %v, want %v", c.code, got, c.untracked)
		}
	}
}
