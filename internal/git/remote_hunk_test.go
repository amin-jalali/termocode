package git

import (
	"context"
	"strings"
	"testing"
)

// remoteHunkDiff is a one-hunk diff of a.go (line 2 changed).
const remoteHunkDiff = `diff --git a/a.go b/a.go
index 1111111..2222222 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package a
-var x = 1
+var x = 2
 // end
`

// inRunner is a recRunner that also takes stdin (an InputRunner, like
// the container Host Exec).
type inRunner struct {
	*recRunner
	stdin map[string]string // "git args…" → what was fed
	code  int
	err   string
}

func (r *inRunner) CaptureInput(_ context.Context, dir string, input []byte, argv ...string) (string, string, int, error) {
	line := strings.Join(argv, " ")
	r.mu.Lock()
	r.calls = append(r.calls, dir+": "+line)
	r.stdin[line] = string(input)
	r.mu.Unlock()
	if r.code != 0 {
		return "", r.err + "\n", r.code, nil
	}
	return "", "", 0, nil
}

func inputRemoteAt(t *testing.T, root string) *inRunner {
	t.Helper()
	r := &inRunner{recRunner: &recRunner{fail: map[string]string{}, out: map[string]string{}}, stdin: map[string]string{}}
	t.Cleanup(SetRemoteRoot(root, r))
	return r
}

// RC-5: hunk staging in a container reads the diff there and feeds the
// one-hunk patch to `git apply` on stdin; no patch file is written.
func TestRemoteStageHunkFeedsThePatchOnStdin(t *testing.T) {
	r := inputRemoteAt(t, "/app")
	r.out["git diff --no-color -- a.go"] = remoteHunkDiff
	if err := StageHunk("/app", "a.go", 2); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git apply --cached -")
	got := r.stdin["git apply --cached -"]
	if !strings.Contains(got, "+var x = 2") || !strings.HasPrefix(got, "diff --git a/a.go b/a.go") {
		t.Fatalf("patch on stdin = %q", got)
	}
}

func TestRemoteUnstageAndDiscardHunkReverse(t *testing.T) {
	r := inputRemoteAt(t, "/app")
	r.out["git diff --no-color --cached -- a.go"] = remoteHunkDiff
	r.out["git diff --no-color -- a.go"] = remoteHunkDiff
	if err := UnstageHunk("/app", "a.go", 2); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git apply --cached --reverse -")
	if err := DiscardHunk("/app", "a.go", 2); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git apply --reverse -")
}

func TestRemoteStageHunkHeaderUsesTheContainerDiff(t *testing.T) {
	r := inputRemoteAt(t, "/app")
	r.out["git -c core.quotepath=off diff --no-color --no-ext-diff --no-textconv -- a.go"] = remoteHunkDiff
	if err := StageHunkHeader("/app", "a.go", "@@ -1,3 +1,3 @@"); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git apply --cached -")
}

func TestRemoteStageLinesRunsInTheContainer(t *testing.T) {
	r := inputRemoteAt(t, "/app")
	r.out["git diff --no-color --no-ext-diff -- a.go"] = remoteHunkDiff
	if err := StageLines("/app", "a.go", 2, 2); err != nil {
		t.Fatal(err)
	}
	r.has(t, "/app: git ls-files --error-unmatch -- a.go")
	r.has(t, "/app: git apply --recount --whitespace=nowarn --cached -")
}

func TestRemoteApplyErrorNamesGitsLine(t *testing.T) {
	r := inputRemoteAt(t, "/app")
	r.out["git diff --no-color -- a.go"] = remoteHunkDiff
	r.code, r.err = 1, "error: patch failed: a.go:1"
	err := StageHunk("/app", "a.go", 2)
	if err == nil || !strings.Contains(err.Error(), "patch failed") {
		t.Fatalf("err = %v", err)
	}
}
