package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// FuzzTreePath: whatever path the page sends, webResolve never returns
// a path outside the root and never panics.
func FuzzTreePath(f *testing.F) {
	for _, s := range []string{"", "a", "../a", "a/../../b", "/etc/passwd", "a//b", `..\x`, "a/./b", "\x00", "ü/..", "...."} {
		f.Add(s)
	}
	base, _ := filepath.EvalSymlinks(f.TempDir())
	root := filepath.Join(base, "ws-root")
	_ = os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	m := &Model{ws: workspace.Workspace{ID: workspace.LocalID(root), Kind: workspace.KindLocal, Root: root}}
	f.Fuzz(func(t *testing.T, p string) {
		abs, err := m.webResolve(p)
		if err != nil {
			return
		}
		if !webInside(root, abs) {
			t.Fatalf("%q resolved to %q, outside %q", p, abs, root)
		}
	})
}
