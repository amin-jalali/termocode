package explorer

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestComputeFolderCountsIgnoresLoadState(t *testing.T) {
	root := &Node{Name: "w", Path: filepath.FromSlash("/w"), IsDir: true}
	j := func(p string) string { return filepath.Join(root.Path, filepath.FromSlash(p)) }
	gs := GitStatus{
		j("README.md"):                        "M",
		j("internal/a/x.go"):                  "M",
		j("internal/b/y.go"):                  "U",
		j("assets"):                           "U", // untracked folder: counts once for its parent
		j("internal/ign.log"):                 "I",
		filepath.FromSlash("/elsewhere/z.go"): "M",
	}
	got := computeFolderCounts([]*Node{root}, gs)
	want := map[string]int{
		root.Path:       4,
		j("internal"):   2,
		j("internal/a"): 1,
		j("internal/b"): 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("computeFolderCounts = %v, want %v", got, want)
	}
}
