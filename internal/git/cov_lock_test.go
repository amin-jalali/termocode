package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitLockedErrorsAreRetriedThenReported(t *testing.T) {
	for _, m := range []string{"x.lock exists", "Another git process", "bad commondir", "cannot lock ref", "Unable to create 'x'"} {
		if !gitLocked(errors.New(m)) {
			t.Errorf("%q should be a lock error", m)
		}
	}
	if gitLocked(nil) || gitLocked(errors.New("fatal: bad object")) {
		t.Error("other errors are not retried")
	}
	oldTries, oldWait := agentLockTries, agentLockWait
	agentLockTries, agentLockWait = 3, time.Millisecond
	t.Cleanup(func() { agentLockTries, agentLockWait = oldTries, oldWait })

	r := committedRepo(t)
	lock := filepath.Join(r.dir, ".git", "refs", "heads", "held.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := gitRetry(r.dir, "update-ref", "refs/heads/held", "HEAD")
	if err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("a held lock is reported after the tries, got %v", err)
	}
	if time.Since(start) < 3*time.Millisecond {
		t.Error("the lock should have been waited for before giving up")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRetry(r.dir, "update-ref", "refs/heads/held", "HEAD"); err != nil {
		t.Errorf("without the lock it works: %v", err)
	}
	if _, err := gitRetry(r.dir, "rev-parse", "--verify", "nope"); err == nil {
		t.Error("a real failure is returned at once")
	}
}

func TestCommitOptsBadAuthorAndOddPaths(t *testing.T) {
	r := committedRepo(t)
	r.write(t, "n.txt", "n\n")
	r.git(t, "add", "n.txt")
	if err := CommitOpts(r.dir, "m", CommitOptions{Author: "no email"}); err == nil {
		t.Error("a bad author: want error")
	}
	odd := "line\nbreak.txt"
	if err := os.WriteFile(filepath.Join(r.dir, odd), []byte("o\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.git(t, "add", "--", odd)
	files, err := StagedFiles(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "\n") {
			t.Errorf("a name with a line break is left out, got %q", f.Path)
		}
	}
	if len(files) != 1 || files[0].Path != "n.txt" {
		t.Errorf("StagedFiles = %+v", files)
	}
}
