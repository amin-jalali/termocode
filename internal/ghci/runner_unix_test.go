//go:build unix

package ghci

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGHBin puts a shell script named gh first on PATH.
func fakeGHBin(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// gh runs in the repo folder with prompts, colours, pager and update
// notes off, and its stdout and stderr come back apart.
func TestDefaultRunnerRunsGH(t *testing.T) {
	fakeGHBin(t, `echo "dir=$(pwd)"; echo "args=$*"; echo "prompt=$GH_PROMPT_DISABLED color=$NO_COLOR notify=$GH_NO_UPDATE_NOTIFIER pager=[$GH_PAGER]"; echo oops >&2`)
	dir := t.TempDir()
	out, errOut, err := DefaultRunner(5*time.Second)(context.Background(), dir, "run", "list", "--limit", "20")
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(out, "dir="+real) && !strings.Contains(out, "dir="+dir) {
		t.Errorf("gh did not run in the repo folder:\n%s", out)
	}
	for _, want := range []string{"args=run list --limit 20", "prompt=1 color=1 notify=1 pager=[]"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q misses %q", out, want)
		}
	}
	if strings.TrimSpace(errOut) != "oops" {
		t.Errorf("stderr = %q", errOut)
	}
}

// gh that hangs (a network stall) is stopped after the timeout.
func TestDefaultRunnerTimeout(t *testing.T) {
	fakeGHBin(t, `exec sleep 30`)
	start := time.Now()
	_, _, err := DefaultRunner(100*time.Millisecond)(context.Background(), t.TempDir(), "pr", "list")
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("the hung gh was not stopped (took %v)", d)
	}
}

// The real runner and a signed-out gh: Fetch reports the quiet error.
func TestFetchWithSignedOutGH(t *testing.T) {
	fakeGHBin(t, `echo "You are not logged into any GitHub hosts. To log in, run: gh auth login" >&2; exit 4`)
	_, err := Fetch(context.Background(), DefaultRunner(5*time.Second), t.TempDir(), "main")
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("err = %v, want ErrNotLoggedIn", err)
	}
}
