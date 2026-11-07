package git

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// recRunner records git commands and answers them from a table.
type recRunner struct {
	mu    sync.Mutex
	calls []string // "dir: git args…"
	fail  map[string]string
	out   map[string]string
}

func (r *recRunner) Capture(_ context.Context, dir string, argv ...string) (string, string, int, error) {
	line := strings.Join(argv, " ")
	r.mu.Lock()
	r.calls = append(r.calls, dir+": "+line)
	r.mu.Unlock()
	if msg, ok := r.fail[line]; ok {
		return "", msg + "\n", 1, nil
	}
	return r.out[line], "", 0, nil
}

func (r *recRunner) has(t *testing.T, want string) {
	t.Helper()
	for _, c := range r.calls {
		if c == want {
			return
		}
	}
	t.Fatalf("no call %q in %q", want, r.calls)
}

func remoteAt(t *testing.T, root string) *recRunner {
	t.Helper()
	r := &recRunner{fail: map[string]string{}, out: map[string]string{}}
	t.Cleanup(SetRemoteRoot(root, r))
	return r
}

func TestRemoteRootRunsWritesThroughTheRunner(t *testing.T) {
	r := remoteAt(t, "/app")
	steps := []struct {
		run  func() error
		want string
	}{
		{func() error { return Stage("/app", "a.go") }, "/app: git add -- a.go"},
		{func() error { return Unstage("/app", "a.go") }, "/app: git restore --staged -- a.go"},
		{func() error { return Commit("/app", "msg") }, "/app: git commit -m msg"},
		{func() error { return Push("/app") }, "/app: git push"},
		{func() error { return PushSetUpstream("/app", "main") }, "/app: git push -u origin main"},
		{func() error { return Pull("/app") }, "/app: git pull"},
		{func() error { return Fetch("/app") }, "/app: git fetch --prune"},
		{func() error { return Checkout("/app", "dev") }, "/app: git checkout dev"},
		{func() error { return CreateBranch("/app/sub", "f") }, "/app/sub: git checkout -b f"},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.want, err)
		}
		r.has(t, s.want)
	}
}

func TestRemoteRootErrorsNameGitsFirstLine(t *testing.T) {
	r := remoteAt(t, "/app")
	r.fail["git push"] = "fatal: no upstream"
	if err := Push("/app"); err == nil || err.Error() != "fatal: no upstream" {
		t.Fatalf("err = %v", err)
	}
	r.fail["git rev-parse --abbrev-ref @{upstream}"] = "fatal: no upstream"
	if HasUpstream("/app") {
		t.Fatal("HasUpstream with a failing rev-parse")
	}
}

func TestRemoteDiscardCleansUntrackedAndRestoresTracked(t *testing.T) {
	r := remoteAt(t, "/app")
	r.out["git status --porcelain=v1 -- new.txt"] = "?? new.txt\n"
	r.out["git status --porcelain=v1 -- a.go"] = " M a.go\n"
	if err := Discard("/app", "new.txt"); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git clean -f -d -- new.txt")
	if err := Discard("/app", "a.go"); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git restore -- a.go")
}

// A Runner that cannot feed stdin still cannot stage one hunk.
func TestRemoteHunkStagingNeedsAnInputRunner(t *testing.T) {
	r := remoteAt(t, "/app")
	r.out["git diff --no-color -- a.go"] = remoteHunkDiff
	if err := StageHunk("/app", "a.go", 2); !errors.Is(err, ErrRemoteUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteRootLeavesOtherFoldersLocal(t *testing.T) {
	remoteAt(t, "/app")
	for _, d := range []string{"/apple", "/", "/home/u/p", ""} {
		if _, ok := remoteFor(d); ok {
			t.Errorf("%q counted as remote", d)
		}
	}
	unset := SetRemoteRoot("/srv", &recRunner{})
	unset()
	if _, ok := remoteFor("/srv"); ok {
		t.Fatal("unset kept the root")
	}
	if SetRemoteRoot("rel", &recRunner{}); func() bool { _, ok := remoteFor("rel"); return ok }() {
		t.Fatal("a relative root was taken")
	}
}
