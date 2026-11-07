package checkpoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain cuts the tests off the developer's git setup.
func TestMain(m *testing.M) {
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		_ = os.Setenv(k, v)
	}
	os.Exit(m.Run())
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	return string(b), err == nil
}

// repo is a git repository with one commit (a.txt, b.txt, .gitignore).
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	run(t, dir, "checkout", "-q", "-b", "main")
	write(t, dir, "a.txt", "a1\n")
	write(t, dir, "b.txt", "b1\n")
	write(t, dir, ".gitignore", "build/\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// Spec: Save keeps the working tree (staged, unstaged and new files) as
// a checkpoint without touching the index, HEAD, the branch or the stash.
func TestSaveLeavesRepoAlone(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", "a2\n")
	write(t, dir, "staged.txt", "s\n")
	run(t, dir, "add", "staged.txt")
	write(t, dir, "new.txt", "n\n")
	status := run(t, dir, "status", "--porcelain")
	headBefore := run(t, dir, "rev-parse", "HEAD")
	p, err := Save(filepath.Join(dir), "before claude turn")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Commit == "" || p.Label != "before claude turn" {
		t.Fatalf("point %+v", p)
	}
	if got := run(t, dir, "status", "--porcelain"); got != status {
		t.Errorf("status changed:\n%s\nwant\n%s", got, status)
	}
	if got := run(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Error("HEAD moved")
	}
	if got := run(t, dir, "stash", "list"); got != "" {
		t.Errorf("stash touched: %q", got)
	}
	files := run(t, dir, "ls-tree", "-r", "--name-only", p.Commit)
	for _, f := range []string{"a.txt", "b.txt", "staged.txt", "new.txt"} {
		if !strings.Contains(files, f) {
			t.Errorf("checkpoint misses %s:\n%s", f, files)
		}
	}
	if got := run(t, dir, "show", p.Commit+":a.txt"); got != "a2\n" {
		t.Errorf("a.txt in checkpoint %q", got)
	}
}

// Spec: Restore puts changed and deleted files back, deletes files made
// after the checkpoint, leaves ignored files alone, and saves the tree
// before it so the restore can be undone.
func TestRestoreAndUndo(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.txt", "mine\n")
	p, err := Save(dir, "start")
	if err != nil {
		t.Fatal(err)
	}
	// The "agent" works.
	write(t, dir, "a.txt", "agent\n")
	_ = os.Remove(filepath.Join(dir, "b.txt"))
	write(t, dir, "deep/x/made.txt", "made\n")
	write(t, dir, "build/out.bin", "keep\n")

	ch, err := Changes(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]byte{}
	for _, c := range ch {
		kinds[c.Path] = c.Kind
	}
	if kinds["a.txt"] != 'M' || kinds["b.txt"] != 'D' || kinds["deep/x/made.txt"] != 'A' || len(kinds) != 3 {
		t.Fatalf("changes %v", kinds)
	}
	if s := Summary(ch); !strings.Contains(s, "1 changed") || !strings.Contains(s, "1 new") || !strings.Contains(s, "1 deleted") {
		t.Errorf("summary %q", s)
	}

	res, err := Restore(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, dir, "a.txt"); got != "mine\n" {
		t.Errorf("a.txt %q", got)
	}
	if got, ok := read(t, dir, "b.txt"); !ok || got != "b1\n" {
		t.Errorf("b.txt not back: %q %v", got, ok)
	}
	if _, ok := read(t, dir, "deep/x/made.txt"); ok {
		t.Error("made.txt still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "deep")); err == nil {
		t.Error("empty folder deep/ left behind")
	}
	if got, ok := read(t, dir, "build/out.bin"); !ok || got != "keep\n" {
		t.Error("ignored file touched")
	}
	if len(res.Removed) != 1 || len(res.Restored) != 2 || res.Undo.ID == "" {
		t.Errorf("result %+v", res)
	}
	if ch, _ := Changes(dir, p); len(ch) != 0 {
		t.Errorf("still differs after restore: %v", ch)
	}
	// The index was not touched: a.txt was never staged.
	if got := run(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("index changed: %q", got)
	}

	// Undo the restore.
	if _, err := Restore(dir, res.Undo); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, dir, "a.txt"); got != "agent\n" {
		t.Errorf("undo: a.txt %q", got)
	}
	if _, ok := read(t, dir, "b.txt"); ok {
		t.Error("undo: b.txt back")
	}
	if got, _ := read(t, dir, "deep/x/made.txt"); got != "made\n" {
		t.Errorf("undo: made.txt %q", got)
	}
}

// Spec: List is newest first with labels; old ones are pruned at
// MaxPoints; Delete drops one.
func TestListPruneDelete(t *testing.T) {
	dir := repo(t)
	old := MaxPoints
	MaxPoints = 3
	t.Cleanup(func() { MaxPoints = old })
	for _, l := range []string{"one", "two", "three", "four"} {
		write(t, dir, "a.txt", l+"\n")
		if _, err := Save(dir, l); err != nil {
			t.Fatal(err)
		}
	}
	pts, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 3 || pts[0].Label != "four" || pts[2].Label != "two" {
		t.Fatalf("list %+v", pts)
	}
	if err := Delete(dir, pts[0].ID); err != nil {
		t.Fatal(err)
	}
	if pts, _ := List(dir); len(pts) != 2 || pts[0].Label != "three" {
		t.Fatalf("after delete %+v", pts)
	}
	if _, err := Find(dir, "nope"); err == nil {
		t.Error("found a checkpoint that is not there")
	}
}

// Spec: a repository without a commit can be checkpointed; a folder
// outside git gets ErrNotRepo.
func TestNoCommitAndNoRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	write(t, dir, "x.txt", "x\n")
	p, err := Save(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Label != "checkpoint" {
		t.Errorf("label %q", p.Label)
	}
	write(t, dir, "x.txt", "y\n")
	if _, err := Restore(dir, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := read(t, dir, "x.txt"); got != "x\n" {
		t.Errorf("x.txt %q", got)
	}
	if _, err := Save(t.TempDir(), "x"); err != ErrNotRepo {
		t.Errorf("err %v, want ErrNotRepo", err)
	}
}

// Spec: the worktrees of one repository share refs, but each lists only
// its own checkpoints (a restore never mixes two trees).
func TestWorktreesKeepTheirOwn(t *testing.T) {
	dir := repo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	run(t, dir, "worktree", "add", "-q", "-b", "side", wt)
	if _, err := Save(dir, "main tree"); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(wt, "side tree"); err != nil {
		t.Fatal(err)
	}
	a, _ := List(dir)
	b, _ := List(wt)
	if len(a) != 1 || a[0].Label != "main tree" || len(b) != 1 || b[0].Label != "side tree" {
		t.Fatalf("main %+v side %+v", a, b)
	}
}
