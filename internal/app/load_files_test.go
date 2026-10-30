package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Spec: Go to file lists every file of the first root by its relative
// path, skips dependency, build and hidden folders (not hidden files),
// lists an extra root's files by absolute path with "<root>/rel" titles,
// and lists a file reachable from two roots once.
func TestLoadFilesIn(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, f := range []string{"x.go", ".env", "sub/y.go", "node_modules/m.js", ".git/HEAD", ".hidden/h.txt", "build/out", "inner/z.go"} {
		p := filepath.Join(a, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "w.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	items := loadFilesIn([]string{a, b, filepath.Join(a, "inner")})
	var got [][2]string
	for _, it := range items {
		if !it.File {
			t.Errorf("%q not marked as a file", it.ID)
		}
		got = append(got, [2]string{it.ID, it.Title})
	}
	want := [][2]string{
		{".env", ".env"},
		{filepath.Join("inner", "z.go"), filepath.Join("inner", "z.go")},
		{filepath.Join("sub", "y.go"), filepath.Join("sub", "y.go")},
		{"x.go", "x.go"},
		{filepath.Join(b, "w.go"), filepath.Join(filepath.Base(b), "w.go")},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("items\n got %q\nwant %q", got, want)
	}
}
