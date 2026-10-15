package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileOf returns the CommitFile for path (fails when missing).
func fileOf(t *testing.T, info CommitInfo, path string) CommitFile {
	t.Helper()
	for _, f := range info.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no %q in %+v", path, info.Files)
	return CommitFile{}
}

// Spec (commit view §4.1): CommitFiles reads a commit's meta and every
// changed file with its status (A M D R), counts and binary flag, in one
// git call; the e-mail is not loaded.
func TestCommitFiles(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "keep.txt", "one\ntwo\nthree\n")
	r.write(t, "gone.txt", "bye\n")
	r.write(t, "old name.txt", strings.Repeat("same line\n", 20))
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "init")
	r.git(t, "tag", "v1")

	r.write(t, "keep.txt", "one\n2\nthree\nfour\n")
	r.write(t, "new.go", "package x\n")
	if err := os.Remove(filepath.Join(r.dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(r.dir, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git(t, "mv", "old name.txt", "dir/new name.txt")
	if err := os.WriteFile(filepath.Join(r.dir, "img.bin"), []byte{0, 1, 2, 0, 255}, 0o644); err != nil {
		t.Fatal(err)
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "change things\n\nA body line.")

	info, err := CommitFiles(r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if info.Subject != "change things" || info.Body != "A body line." || info.Author != "Test" || info.Email != "" {
		t.Errorf("meta %+v", info.CommitDetail)
	}
	if len(info.FullHash) != 40 || !strings.HasPrefix(info.FullHash, info.Hash) || len(info.Parents) != 1 {
		t.Errorf("hashes %q %q parents %v", info.Hash, info.FullHash, info.Parents)
	}
	if info.DateAbs == "" || info.DateRel == "" {
		t.Errorf("dates %q %q", info.DateAbs, info.DateRel)
	}
	if len(info.Files) != 5 || info.CommitDetail.Files != 5 {
		t.Fatalf("files %+v", info.Files)
	}
	if f := fileOf(t, info, "keep.txt"); f.Status != 'M' || f.Add != 2 || f.Del != 1 {
		t.Errorf("keep.txt %+v", f)
	}
	if f := fileOf(t, info, "new.go"); f.Status != 'A' || f.Add != 1 || f.Del != 0 {
		t.Errorf("new.go %+v", f)
	}
	if f := fileOf(t, info, "gone.txt"); f.Status != 'D' || f.Del != 1 {
		t.Errorf("gone.txt %+v", f)
	}
	if f := fileOf(t, info, "dir/new name.txt"); f.Status != 'R' || f.OldPath != "old name.txt" || f.Add != 0 {
		t.Errorf("rename %+v", f)
	}
	if f := fileOf(t, info, "img.bin"); !f.Binary || f.Status != 'A' {
		t.Errorf("binary %+v", f)
	}
	if info.Add != 3 || info.Del != 2 {
		t.Errorf("totals +%d −%d, want +3 −2", info.Add, info.Del)
	}
	// The first commit has no parent and lists its tag.
	root, err := CommitFiles(r.dir, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Parents) != 0 || len(root.Files) != 3 || len(root.Refs) != 1 || root.Refs[0] != (Ref{Name: "v1", Kind: RefTag}) {
		t.Errorf("root %+v", root)
	}
}

// Spec: a merge commit lists what it brought in against its first parent.
func TestCommitFilesMergeUsesFirstParent(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "init")
	r.git(t, "checkout", "-q", "-b", "topic")
	r.write(t, "topic.txt", "a\nb\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "topic")
	r.git(t, "checkout", "-q", "main")
	r.commitN(t, "main work")
	r.git(t, "merge", "-q", "--no-ff", "-m", "Merge topic", "topic")
	info, err := CommitFiles(r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Parents) != 2 || len(info.Files) != 1 || info.Files[0].Path != "topic.txt" || info.Files[0].Add != 2 {
		t.Errorf("merge %+v", info)
	}
}

// Spec (§4.1): Revert makes a new "Revert …" commit.
func TestRevert(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "one")
	r.commitN(t, "two")
	if _, err := Revert(r.dir, "HEAD"); err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); s != `Revert "two"` {
		t.Errorf("subject %q", s)
	}
	if b, _ := os.ReadFile(filepath.Join(r.dir, "f.txt")); string(b) != "one\n" {
		t.Errorf("f.txt = %q", b)
	}
}

// Spec: a revert that conflicts reports the conflicted paths and leaves
// the repo in the revert state (CONFLICTS shows them).
func TestRevertConflictReportsPaths(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "one")
	r.commitN(t, "two")
	r.commitN(t, "three") // reverting "two" now conflicts on f.txt
	paths, err := Revert(r.dir, "HEAD~1")
	if err == nil {
		t.Fatal("no error for a conflicting revert")
	}
	if len(paths) != 1 || paths[0] != "f.txt" {
		t.Errorf("conflicts %v", paths)
	}
}

// Spec: CherryPick copies a commit from another branch onto HEAD.
func TestCherryPick(t *testing.T) {
	r := newGitTestRepo(t)
	r.commitN(t, "init")
	r.git(t, "checkout", "-q", "-b", "topic")
	r.write(t, "pick.txt", "picked\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "pick me")
	r.git(t, "checkout", "-q", "main")
	if _, err := CherryPick(r.dir, "topic"); err != nil {
		t.Fatal(err)
	}
	if s := strings.TrimSpace(r.git(t, "log", "-1", "--format=%s")); s != "pick me" {
		t.Errorf("subject %q", s)
	}
}
