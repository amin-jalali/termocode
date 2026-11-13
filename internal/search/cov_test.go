package search

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// memFS is a search.FS (and WriteFS) over an in-memory tree.
type memFS struct {
	m          fstest.MapFS
	dirErr     map[string]error // ReadDir errors by path
	fileErr    map[string]error // ReadFile errors by path
	cancel     context.CancelFunc
	cancelOnRd string // cancel the context when this file is read
	written    map[string]string
}

func (f *memFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	if err := f.dirErr[p]; err != nil {
		return nil, err
	}
	return f.m.ReadDir(strings.TrimPrefix(path.Clean(p), "/"))
}

func (f *memFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	if f.cancel != nil && p == f.cancelOnRd {
		f.cancel()
	}
	if err := f.fileErr[p]; err != nil {
		return nil, err
	}
	return f.m.ReadFile(strings.TrimPrefix(path.Clean(p), "/"))
}

func (f *memFS) WriteFile(_ context.Context, p string, data []byte, _ fs.FileMode) error {
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[p] = string(data)
	return nil
}

func sampleFS() *memFS {
	return &memFS{m: fstest.MapFS{
		"proj/a.go":              {Data: []byte("package a\nfunc Needle() {}\n")},
		"proj/b.txt":             {Data: []byte("a needle here\nand another needle\n")},
		"proj/sub/c.go":          {Data: []byte("// needle in sub\n")},
		"proj/node_modules/x.js": {Data: []byte("needle skipped\n")},
		"proj/bin.dat":           {Data: []byte("needle\x00binary")},
		"proj/big.txt":           {Data: []byte(strings.Repeat("needle ", maxFallbackFileSize/7+10))},
		"proj/none.txt":          {Data: []byte("nothing to see\n")},
		"proj/dir/in.txt":        {Data: []byte("needle in dir\n")},
	}}
}

func paths(rs []Result) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range rs {
		if !seen[r.Path] {
			seen[r.Path] = true
			out = append(out, r.Path)
		}
	}
	return out
}

// A search over a workspace FS walks it in name order, skips dependency
// folders, binary and huge files, and gives absolute paths.
func TestSearchOverFS(t *testing.T) {
	fsys := sampleFS()
	rs, sum, err := Collect(context.Background(), []string{"/proj"}, "needle", Options{FS: fsys})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(rs), ",")
	want := "/proj/a.go,/proj/b.txt,/proj/dir/in.txt,/proj/sub/c.go"
	if got != want {
		t.Fatalf("files %s, want %s", got, want)
	}
	if sum.Files != 4 || sum.Total != 5 || sum.Shown != 5 {
		t.Fatalf("summary %+v", sum)
	}
}

func TestSearchOverFSFilters(t *testing.T) {
	fsys := sampleFS()
	rs, _, err := Collect(context.Background(), []string{"/proj"}, "needle", Options{FS: fsys, Include: []string{"*.go"}, Exclude: []string{"sub"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(rs), ","); got != "/proj/a.go" {
		t.Fatalf("got %s", got)
	}
}

func TestSearchOverFSResultCap(t *testing.T) {
	rs, sum, err := Collect(context.Background(), []string{"/proj"}, "needle", Options{FS: sampleFS(), MaxResults: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || sum.Shown != 2 || sum.Total != 5 || !sum.Truncated() {
		t.Fatalf("rs %d sum %+v", len(rs), sum)
	}
}

// A bad query is an error; an unreadable root is an error; an unreadable
// sub folder or file is skipped.
func TestSearchOverFSErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	if _, _, err := Collect(ctx, []string{"/proj"}, "(", Options{FS: sampleFS(), Regex: true}); err == nil {
		t.Fatal("bad regex accepted")
	}
	f := sampleFS()
	f.dirErr = map[string]error{"/proj": boom}
	if _, _, err := Collect(ctx, []string{"/proj"}, "needle", Options{FS: f}); !errors.Is(err, boom) {
		t.Fatalf("root error: %v", err)
	}
	f = sampleFS()
	f.dirErr = map[string]error{"/proj/sub": boom}
	f.fileErr = map[string]error{"/proj/a.go": boom}
	rs, _, err := Collect(ctx, []string{"/proj"}, "needle", Options{FS: f})
	if err != nil || strings.Join(paths(rs), ",") != "/proj/b.txt,/proj/dir/in.txt" {
		t.Fatalf("skipped: %v %v", paths(rs), err)
	}
}

// A cancelled search stops, also in the middle of a file read.
func TestSearchOverFSCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Collect(ctx, []string{"/proj"}, "needle", Options{FS: sampleFS()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	f := sampleFS()
	f.cancel, f.cancelOnRd = cancel, "/proj/b.txt"
	f.fileErr = map[string]error{"/proj/b.txt": errors.New("gone")}
	if _, _, err := Collect(ctx, []string{"/proj"}, "needle", Options{FS: f}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled during read: %v", err)
	}
}

func TestCountLiteralFSEdgeCases(t *testing.T) {
	ctx := context.Background()
	if got, err := CountLiteralFS(ctx, sampleFS(), "", "x"); got != nil || err != nil {
		t.Fatal("empty root")
	}
	boom := errors.New("boom")
	f := sampleFS()
	got, err := CountLiteralFS(ctx, f, "/proj", "needle")
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	f = sampleFS()
	f.dirErr = map[string]error{"/proj/sub": boom}
	f.fileErr = map[string]error{"/proj/a.go": boom}
	got, err = CountLiteralFS(ctx, f, "/proj", "needle")
	if err != nil || len(got) != 2 {
		t.Fatalf("skipped: %v %v", got, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	f = sampleFS()
	f.cancel, f.cancelOnRd = cancel, "/proj/b.txt"
	f.fileErr = map[string]error{"/proj/b.txt": boom}
	if _, err := CountLiteralFS(cctx, f, "/proj", "needle"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel mid read: %v", err)
	}
	cctx, cancel = context.WithCancel(ctx)
	cancel()
	if _, err := CountLiteralFS(cctx, sampleFS(), "/proj", "needle"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	// Special files are not read.
	f = sampleFS()
	f.m["proj/link"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("needle")}
	if got, _ := CountLiteralFS(ctx, f, "/proj", "needle"); len(got) != 3 {
		t.Fatalf("symlink counted: %v", got)
	}
}

func TestReplaceLiteralInFileFSEdgeCases(t *testing.T) {
	ctx := context.Background()
	f := sampleFS()
	if n, err := ReplaceLiteralInFileFS(ctx, f, "/proj/b.txt", "", "x"); n != 0 || err != nil {
		t.Fatal("empty find")
	}
	if _, err := ReplaceLiteralInFileFS(ctx, f, "/proj/missing", "a", "b"); err == nil {
		t.Fatal("missing file")
	}
	n, err := ReplaceLiteralInFileFS(ctx, f, "/proj/b.txt", "needle", "pin")
	if err != nil || n != 2 || !strings.Contains(f.written["/proj/b.txt"], "pin") {
		t.Fatalf("%d %v %v", n, err, f.written)
	}
	if n, _ := ReplaceLiteralInFileFS(ctx, f, "/proj/b.txt", "zzz", "q"); n != 0 {
		t.Fatal("no match counted")
	}
	wfs := &failWrite{memFS: sampleFS()}
	if _, err := ReplaceLiteralInFileFS(ctx, wfs, "/proj/b.txt", "needle", "pin"); err == nil {
		t.Fatal("write error swallowed")
	}
}

type failWrite struct{ *memFS }

func (*failWrite) WriteFile(context.Context, string, []byte, fs.FileMode) error {
	return errors.New("read-only")
}

// Files without ripgrep: blank globs are ignored, hidden files and
// dependency folders are left out, excludes drop whole folders.
func TestFilesWalkerWithoutRipgrep(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := t.TempDir()
	for p, body := range map[string]string{
		"a.go": "x", "sub/b.go": "x", "sub/c.txt": "x", ".hidden": "x", "node_modules/m.js": "x", "skip/d.go": "x",
	} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a.go", filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	files, total, err := Files(context.Background(), root, []string{" ", "*.go", "!skip"}, 0)
	if err != nil || strings.Join(files, ",") != "a.go,link.go,sub/b.go" || total != 3 {
		t.Fatalf("%v %d %v", files, total, err)
	}
	files, total, _ = Files(context.Background(), root, nil, 2)
	if len(files) != 2 || total < 4 {
		t.Fatalf("cap: %v %d", files, total)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Files(cctx, root, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

// With a ripgrep that fails, its message is the error; a plain "no files"
// exit is not an error; a cancelled run reports the cancel.
func TestFilesRipgrepFailures(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	write := func(script string) {
		if err := os.WriteFile(filepath.Join(bin, "rg"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("echo 'rg: bad glob' >&2\necho second >&2\nexit 2\n")
	if _, _, err := Files(context.Background(), dir, nil, 0); err == nil || err.Error() != "rg: bad glob" {
		t.Fatalf("stderr message: %v", err)
	}
	write("exit 2\n")
	if _, _, err := Files(context.Background(), dir, nil, 0); err == nil {
		t.Fatal("silent failure swallowed")
	}
	write("exit 1\n")
	if files, _, err := Files(context.Background(), dir, nil, 0); err != nil || len(files) != 0 {
		t.Fatalf("no files: %v %v", files, err)
	}
	write("printf './b.go\\na.go\\n\\n'\n")
	files, _, _ := Files(context.Background(), dir, []string{"*.go", "!x"}, 0)
	if strings.Join(files, ",") != "a.go,b.go" {
		t.Fatalf("listing: %v", files)
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Files(cctx, dir, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestMatchGlob(t *testing.T) {
	for _, c := range []struct {
		glob, rel string
		want      bool
	}{
		{"*.go", "a/b/c.go", true}, {"*.go", "a/b/c.txt", false}, {"sub/**", "sub/x/y.go", true}, {"sub/**", "other/x.go", false},
	} {
		if MatchGlob(c.glob, c.rel) != c.want {
			t.Errorf("MatchGlob(%q, %q) != %v", c.glob, c.rel, c.want)
		}
	}
}
