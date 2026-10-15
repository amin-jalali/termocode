package shotkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Spec: the demo workspace (screenshots, the README GIF) holds only the
// repository's tracked files. Untracked files in the developer's checkout
// (private notes, local builds) must never end up in a published image.
func TestDemoWorkspaceOnlyTrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = src
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, s string) {
		p := filepath.Join(src, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("README.md", "hi\n")
	write("internal/statusbar/model.go", "package statusbar\n")
	write("internal/theme/palette.go", "package theme\n")
	write("internal/toast/model.go", "package toast\n")
	run("add", ".")
	run("commit", "-qm", "init")
	write("private-notes.txt", "secret\n")
	write("termocode", "binary\n")

	dst, err := DemoWorkspace(src, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"private-notes.txt", "termocode"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err == nil {
			t.Errorf("untracked %s was copied into the demo workspace", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "internal/statusbar/model.go")); err != nil {
		t.Errorf("tracked file missing: %v", err)
	}
}
