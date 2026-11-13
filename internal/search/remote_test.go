package search

import (
	"context"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// mapFS is a search FS over an in-memory tree rooted at /app.
type mapFS struct {
	m     fstest.MapFS
	reads []string
}

func (f *mapFS) rel(p string) string { return strings.TrimPrefix(strings.TrimPrefix(p, "/app"), "/") }

func (f *mapFS) ReadDir(_ context.Context, p string) ([]fs.DirEntry, error) {
	r := f.rel(p)
	if r == "" {
		r = "."
	}
	return fs.ReadDir(f.m, r)
}

func (f *mapFS) ReadFile(_ context.Context, p string) ([]byte, error) {
	f.reads = append(f.reads, f.rel(p))
	return fs.ReadFile(f.m, f.rel(p))
}

// fakeRemote answers rg or grep in /app with the files that hold the
// needle (a superset, like the real prefilter), or fails like a missing
// command.
type fakeRemote struct {
	m     fstest.MapFS
	argvs [][]string
	code  int
}

func (r *fakeRemote) Capture(_ context.Context, dir string, argv ...string) (string, string, int, error) {
	r.argvs = append(r.argvs, append([]string(nil), argv...))
	if r.code != 0 {
		return "", "sh: " + argv[0] + ": not found", r.code, nil
	}
	needle := strings.ToLower(argv[len(argv)-1])
	if argv[0] == "grep" {
		needle = strings.ToLower(argv[len(argv)-2])
	}
	var hits []string
	_ = fs.WalkDir(r.m, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(r.m, p)
			if strings.Contains(strings.ToLower(string(b)), needle) {
				hits = append(hits, p)
			}
		}
		return nil
	})
	sort.Strings(hits)
	if len(hits) == 0 {
		return "", "", 1, nil
	}
	if argv[0] == "rg" {
		return strings.Join(hits, "\x00") + "\x00", "", 0, nil
	}
	for i := range hits {
		hits[i] = "./" + hits[i]
	}
	return strings.Join(hits, "\n") + "\n", "", 0, nil
}

func remoteTree() fstest.MapFS {
	return fstest.MapFS{
		"main.go":           {Data: []byte("package main\n// Needle here\n")},
		"pkg/a.go":          {Data: []byte("package pkg\n")},
		"pkg/b.go":          {Data: []byte("package pkg\nvar x = \"needle\"\n")},
		"node_modules/x.js": {Data: []byte("needle")},
		"docs/readme.txt":   {Data: []byte("nothing")},
	}
}

func remoteSearch(t *testing.T, tools []string, rem *fakeRemote, query string, opts Options) ([]string, *mapFS) {
	t.Helper()
	fsys := &mapFS{m: remoteTree()}
	opts.FS, opts.Remote, opts.RemoteTools = fsys, rem, tools
	var got []string
	if _, err := Search(context.Background(), []string{"/app"}, query, opts, func(rs []Result) {
		for _, r := range rs {
			got = append(got, path.Base(r.Path)+":"+strings.TrimSpace(r.Preview))
		}
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	return got, fsys
}

// RC-8/RC-9: in a container workspace the search asks rg in the image
// (or in the tools sidecar) which files hold the text, else grep, and
// reads only those files; skipped folders stay skipped. Without either
// it walks every file as before.
func TestRemoteSearchPrefiltersWithRgThenGrep(t *testing.T) {
	want := "b.go:var x = \"needle\",main.go:// Needle here"
	for _, tool := range []string{"rg", "grep"} {
		rem := &fakeRemote{m: remoteTree()}
		got, fsys := remoteSearch(t, []string{tool}, rem, "needle", Options{})
		if strings.Join(got, ",") != want {
			t.Fatalf("%s: results %q", tool, got)
		}
		if len(rem.argvs) != 1 || rem.argvs[0][0] != tool {
			t.Fatalf("%s: commands %q", tool, rem.argvs)
		}
		sort.Strings(fsys.reads)
		if strings.Join(fsys.reads, ",") != "main.go,pkg/b.go" {
			t.Fatalf("%s: read %q, want only the files with the text", tool, fsys.reads)
		}
	}
}

func TestRemoteSearchWithoutToolsWalks(t *testing.T) {
	rem := &fakeRemote{m: remoteTree()}
	got, fsys := remoteSearch(t, nil, rem, "needle", Options{})
	if len(got) != 2 || len(rem.argvs) != 0 || len(fsys.reads) < 3 {
		t.Fatalf("results %q, commands %q, reads %q", got, rem.argvs, fsys.reads)
	}
}

// grep's regex is not Go's: a regex search with only grep walks; a
// failing command (gone from the image) walks too.
func TestRemoteSearchFallsBackToTheWalker(t *testing.T) {
	rem := &fakeRemote{m: remoteTree()}
	if got, _ := remoteSearch(t, []string{"grep"}, rem, "need.e", Options{Regex: true}); len(got) != 2 || len(rem.argvs) != 0 {
		t.Fatalf("regex with grep: %q %q", got, rem.argvs)
	}
	rem = &fakeRemote{m: remoteTree(), code: 127}
	if got, _ := remoteSearch(t, []string{"rg"}, rem, "needle", Options{}); len(got) != 2 {
		t.Fatalf("failed rg: %q", got)
	}
}
