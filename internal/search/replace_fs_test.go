package search

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// diskFS is a WriteFS on the local disk that counts its calls, standing
// in for a container's Host FS.
type diskFS struct{ reads, writes int }

func (d *diskFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) { return os.ReadDir(p) }
func (d *diskFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	d.reads++
	return os.ReadFile(p)
}
func (d *diskFS) WriteFile(_ context.Context, p string, data []byte, perm fs.FileMode) error {
	d.writes++
	return os.WriteFile(p, data, perm)
}

func TestCountLiteralFSCountsThroughTheFS(t *testing.T) {
	root := t.TempDir()
	write := func(rel, s string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "foo foo bar")
	write("sub/b.go", "foo")
	write("c.go", "nothing")
	write("node_modules/x.js", "foo")
	write("bin.dat", "foo\x00foo")
	fsys := &diskFS{}
	got, err := CountLiteralFS(context.Background(), fsys, root, "foo")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{filepath.Join(root, "a.go"): 2, filepath.Join(root, "sub/b.go"): 1}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, c := range got {
		if want[c.Path] != c.Count {
			t.Fatalf("got %+v", got)
		}
	}
	if fsys.reads == 0 {
		t.Fatal("nothing was read through the FS")
	}
}

func TestCountLiteralFSMissingRootIsAnError(t *testing.T) {
	if _, err := CountLiteralFS(context.Background(), &diskFS{}, filepath.Join(t.TempDir(), "gone"), "x"); err == nil {
		t.Fatal("a missing root must fail")
	}
}

func TestReplaceLiteralInFileFSWritesOnceAndCounts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("a-b-a"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsys := &diskFS{}
	n, err := ReplaceLiteralInFileFS(context.Background(), fsys, p, "a", "zz")
	if err != nil || n != 2 || fsys.writes != 1 {
		t.Fatalf("n %d err %v writes %d", n, err, fsys.writes)
	}
	if b, _ := os.ReadFile(p); string(b) != "zz-b-zz" {
		t.Fatalf("file = %q", b)
	}
	n, err = ReplaceLiteralInFileFS(context.Background(), fsys, p, "nope", "x")
	if err != nil || n != 0 || fsys.writes != 1 {
		t.Fatalf("no match: n %d err %v writes %d", n, err, fsys.writes)
	}
}
