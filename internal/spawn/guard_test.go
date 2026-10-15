package spawn

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoDirectExec keeps every child process on the Android trampoline:
// app code must use spawn.Command / CommandContext / Exec / Executable,
// not the os/exec, syscall or os versions.
func TestNoDirectExec(t *testing.T) {
	direct := regexp.MustCompile(`\bexec\.Command(Context)?\(|\bsyscall\.Exec\(|\bos\.Executable\(\)`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case d.Name() == "third_party", d.Name() == "e2e", d.Name() == ".git", d.Name() == "node_modules",
				d.Name() == ".claude",                            // agent worktrees: other checkouts
				path == filepath.Join(root, "internal", "spawn"): // this package
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if direct.MatchString(code) {
				t.Errorf("%s:%d: use the spawn package: %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
