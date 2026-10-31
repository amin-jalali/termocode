package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Spec: HeadSHA is the 40-hex sha of the commit HEAD points at; an empty
// repository is an error.
func TestHeadSHA(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	g := func(args ...string) string {
		t.Helper()
		out, err := gitOut(dir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	g("init", "-q")
	if _, err := HeadSHA(dir); err == nil {
		t.Error("empty repo: no error")
	}
	os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0o644)
	g("add", "a")
	g("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "one")
	sha, err := HeadSHA(dir)
	if err != nil || len(sha) != 40 {
		t.Fatalf("sha %q %v", sha, err)
	}
}
