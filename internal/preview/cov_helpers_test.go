package preview_test

import (
	"os"
	"testing"
)

func writeFile(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, p string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func chmod(p string, mode os.FileMode) { _ = os.Chmod(p, mode) }

func isRoot() bool { return os.Geteuid() == 0 }
