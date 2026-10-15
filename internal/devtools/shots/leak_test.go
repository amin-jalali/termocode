package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The demo workspace must not outlive the run: after cleanup, no
// termocode-demo-* folder is left in TMPDIR.
func TestEnterDemoWorkspaceLeavesNoTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	old, _ := os.Getwd()
	repo, _ := filepath.Abs("../shotkit/testdata/workspace")
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	cleanup, err := enterDemoWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	left, _ := filepath.Glob(filepath.Join(tmp, "termocode-*"))
	if len(left) != 0 {
		t.Fatalf("temp dirs left behind: %v", left)
	}
}
