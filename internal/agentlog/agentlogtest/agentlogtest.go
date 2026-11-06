// Package agentlogtest lays out the agentlog fixture transcripts as a
// fake home folder for tests: <home>/.claude/projects/<slug>/… and
// <home>/.codex/sessions/…. Tests never read the real home folders.
package agentlogtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Folder is the folder the fixture sessions ran in.
const Folder = "/work/demo"

// testdata is internal/agentlog/testdata.
func testdata() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "testdata")
}

// FakeHome copies the fixtures into a new temp folder and returns it.
func FakeHome(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	copyTree(t, filepath.Join(testdata(), "claude"), filepath.Join(home, ".claude", "projects"))
	copyTree(t, filepath.Join(testdata(), "codex"), filepath.Join(home, ".codex"))
	return home
}

func copyTree(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
