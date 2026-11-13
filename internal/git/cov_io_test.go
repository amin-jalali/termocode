package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebasePlanNeedsATempFolder(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	if _, _, err := writeRebasePlan(rebasePlan{}); err == nil {
		t.Error("no temp folder: want error")
	}
	r, h := rebaseRepo(t, 2)
	if _, err := RebaseOnto(r.dir, h[0], false, RebaseOptions{}); err == nil {
		t.Error("a rebase that cannot keep its plan must say so, not start")
	}
	if headOf(t, r) != h[1] {
		t.Error("the failed rebase moved HEAD")
	}
}

func TestRunCappedCutsAtALineEnd(t *testing.T) {
	r := committedRepo(t)
	// The first line is longer than the cap: nothing whole fits.
	out, cut, err := runCapped(r.dir, 2, false, "cat-file", "-p", "HEAD:a.txt")
	if err != nil || !cut || out != "" {
		t.Errorf("runCapped = %q, %v, %v", out, cut, err)
	}
	out, cut, err = runCapped(r.dir, 8, false, "cat-file", "-p", "HEAD:a.txt")
	if err != nil || !cut || out != "one\ntwo\n" {
		t.Errorf("runCapped = %q, %v, %v", out, cut, err)
	}
	if _, _, err := runCapped(r.dir, 100, false, "cat-file", "-p", "HEAD:nope"); err == nil {
		t.Error("a failing git: want error")
	}
	if _, _, err := runCapped(filepath.Join(r.dir, "no-such-dir"), 100, false, "status"); err == nil {
		t.Error("a missing folder: want error")
	}
}

func TestShowCappedTooBigAndHunkTooBig(t *testing.T) {
	r := committedRepo(t)
	if _, _, cut, err := ShowCapped(r.dir, "HEAD", 1); err == nil || !cut {
		t.Errorf("a commit over the cap = cut %v, %v", cut, err)
	}
}

func readOnlyRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
}

func TestMergeAgentWorktreeReportsAWriteFailure(t *testing.T) {
	readOnlyRoot(t)
	r := committedRepo(t)
	w, err := CreateAgentWorktree(r.dir, "ro1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(w.Path, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "sub", "n.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "b.txt"), []byte("bee changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(r.dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(r.dir, 0o755) })
	if _, err := MergeAgentWorktree(r.dir, "ro1", false); err == nil {
		t.Error("the user's folder cannot be written: want error")
	}
	_ = os.Chmod(r.dir, 0o755)
	res, err := MergeAgentWorktree(r.dir, "ro1", false)
	if err != nil || len(res.Applied) != 2 {
		t.Fatalf("MergeAgentWorktree = %+v, %v", res, err)
	}
	if got, _ := os.ReadFile(filepath.Join(r.dir, "sub", "n.txt")); string(got) != "n\n" {
		t.Errorf("n.txt = %q", got)
	}
	if err := RemoveAgentWorktree(r.dir, "ro1", true); err != nil {
		t.Error(err)
	}
}

func TestMergeAgentWorktreeUnknownAndForce(t *testing.T) {
	r := committedRepo(t)
	if _, err := MergeAgentWorktree(r.dir, "never-made", false); err == nil || !strings.Contains(err.Error(), "not known") {
		t.Errorf("unknown lane = %v", err)
	}
	w, err := CreateAgentWorktree(r.dir, "f1")
	if err != nil {
		t.Fatal(err)
	}
	// The agent edits a.txt and adds a binary; the user edits a.txt too.
	if err := os.WriteFile(filepath.Join(w.Path, "a.txt"), []byte("one\nagent\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "bin.dat"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	r.write(t, "a.txt", "one\nuser\nthree\n")
	r.write(t, "bin.dat", "\x00user\n") // the user has a different file there already
	res, err := MergeAgentWorktree(r.dir, "f1", false)
	if err != nil || len(res.Conflicts) != 2 || len(res.Applied) != 0 {
		t.Fatalf("plain merge = %+v, %v", res, err)
	}
	res, err = MergeAgentWorktree(r.dir, "f1", true)
	if err != nil || len(res.Kept) != 1 || res.Kept[0] != "bin.dat" {
		t.Fatalf("forced merge = %+v, %v", res, err)
	}
	got, _ := os.ReadFile(filepath.Join(r.dir, "a.txt"))
	if !strings.Contains(string(got), "<<<<<<< yours") || !strings.Contains(string(got), ">>>>>>> agent f1") {
		t.Errorf("conflict markers missing:\n%s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "bin.dat")); string(b) != "\x00user\n" {
		t.Errorf("the user's binary must stay, got %q", b)
	}
}

func TestSetAgentExecBits(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(f, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := setAgentExec(f, 0o640, "100644", "100755"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm() != 0o750 {
		t.Errorf("after +x: %v", fi.Mode().Perm())
	}
	if err := setAgentExec(f, 0o750, "100755", "100644"); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm() != 0o640 {
		t.Errorf("after -x: %v", fi.Mode().Perm())
	}
	if err := setAgentExec(f, 0o640, "100644", "100644"); err != nil {
		t.Errorf("no mode change: %v", err)
	}
}
