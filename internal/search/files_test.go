package search

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func filesTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, p := range []string{"a.go", "a_test.go", "sub/b.go", "sub/deep/c_test.go", "docs/x.md", "node_modules/m.go", ".git/HEAD"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFilesFallbackMatchesGlobs(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no rg: the Go walker runs
	dir := filesTree(t)
	for _, tc := range []struct {
		globs []string
		want  []string
	}{
		{[]string{"*_test.go"}, []string{"a_test.go", "sub/deep/c_test.go"}},
		{[]string{"**/*.go"}, []string{"a.go", "a_test.go", "sub/b.go", "sub/deep/c_test.go"}},
		{[]string{"sub/**"}, []string{"sub/b.go", "sub/deep/c_test.go"}},
		{[]string{"*.go", "!*_test.go"}, []string{"a.go", "sub/b.go"}},
		{[]string{"*.md"}, []string{"docs/x.md"}},
	} {
		got, total, err := Files(context.Background(), dir, tc.globs, 100)
		if err != nil {
			t.Fatalf("%v: %v", tc.globs, err)
		}
		if !reflect.DeepEqual(got, tc.want) || total != len(tc.want) {
			t.Errorf("%v: got %v (total %d), want %v", tc.globs, got, total, tc.want)
		}
	}
}

func TestFilesCapsAndCounts(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := filesTree(t)
	got, total, err := Files(context.Background(), dir, []string{"*.go"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "a_test.go"}; !reflect.DeepEqual(got, want) || total != 4 {
		t.Errorf("got %v total %d, want %v total 4", got, total, want)
	}
}

func TestFilesStopsOnCancel(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := filesTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Files(ctx, dir, []string{"*.go"}, 10); err == nil {
		t.Error("a stopped listing gave no error")
	}
}

// A fake rg proves the ripgrep path: its file list is filtered by
// nothing more, sorted and cut like the walker's.
func TestFilesUsesRipgrepWhenInstalled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake")
	}
	bin := t.TempDir()
	args := filepath.Join(bin, "args")
	script := "#!/bin/sh\necho \"$@\" > " + args + "\nprintf 'z.go\\nb/y.go\\nx.go\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "rg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	got, total, err := Files(context.Background(), t.TempDir(), []string{"*.go", "!vendor"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b/y.go", "x.go"}; !reflect.DeepEqual(got, want) || total != 3 {
		t.Errorf("got %v total %d, want %v total 3", got, total, want)
	}
	b, _ := os.ReadFile(args)
	if want := "--files --glob=*.go --glob=!vendor\n"; string(b) != want {
		t.Errorf("rg args = %q, want %q", b, want)
	}
}
