package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/lspmirror"
)

// activeTestMirror is an active mirror of /app holding files.
func activeTestMirror(t *testing.T, files map[string]string) *lspmirror.Mirror {
	t.Helper()
	mr := lspmirror.New(t.TempDir(), "k", "/app")
	if err := os.MkdirAll(mr.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for p, body := range files {
		if err := mr.Put(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	old := activeLSPMirror.Load()
	activeLSPMirror.Store(mr)
	t.Cleanup(func() { activeLSPMirror.Store(old) })
	return mr
}

// RC-6: a rename, move or delete in the explorer of a container
// workspace updates the local mirror at once, so a local language
// server never keeps a file that is gone or misses one under its new
// name.
func TestContainerRenameAndDeleteUpdateTheMirror(t *testing.T) {
	m, _, _, _ := newContainerModel(t)
	mr := activeTestMirror(t, map[string]string{"/app/main.go": "package main\n", "/app/pkg/util.go": "package pkg\n"})
	m.promptKind, m.promptPath = promptKindRename, "/app/main.go"
	runRemoteOp(t, &m, m.handlePromptSubmit("app.go"))
	if _, err := os.Stat(filepath.Join(mr.Dir, "main.go")); !os.IsNotExist(err) {
		t.Fatalf("mirror keeps the old name: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(mr.Dir, "app.go")); err != nil || string(b) != "package main\n" {
		t.Fatalf("mirror new name = %q %v", b, err)
	}
	m.openDeleteConfirm("/app/pkg")
	runRemoteOp(t, &m, m.handleConfirmSelect("delete"))
	if _, err := os.Stat(filepath.Join(mr.Dir, "pkg")); !os.IsNotExist(err) {
		t.Fatalf("mirror keeps a deleted folder: %v", err)
	}
}

func TestMirrorRenameMovesAndLeavesSkippedPaths(t *testing.T) {
	mr := activeTestMirror(t, map[string]string{"/app/a/x.go": "x"})
	if err := mr.Rename("/app/a", "/app/b"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(mr.Dir, "b", "x.go")); err != nil || string(b) != "x" {
		t.Fatalf("moved = %q %v", b, err)
	}
	// Into a left-out folder: the copy just goes.
	if err := mr.Rename("/app/b", "/app/node_modules/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mr.Dir, "b")); !os.IsNotExist(err) {
		t.Fatalf("copy kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mr.Dir, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("a left-out folder was made: %v", err)
	}
}
