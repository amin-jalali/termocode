package librarian

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CMP-91: an outside repository is named owner/name (GitHub), host/
// owner/name or an https URL; anything that could reach a local file,
// another protocol, a private host or carry a password is refused.
func TestParse(t *testing.T) {
	good := map[string]string{
		"golang/go":                              "github.com/golang/go",
		"github.com/charmbracelet/bubbletea":     "github.com/charmbracelet/bubbletea",
		"https://github.com/charmbracelet/glow":  "github.com/charmbracelet/glow",
		"https://gitlab.com/a/b.git":             "gitlab.com/a/b",
		"https://github.com/a/b/":                "github.com/a/b",
		"  https://codeberg.org/x.y/z_1  ":       "codeberg.org/x.y/z_1",
		"https://github.com/a/b/tree/main/cmd/x": "github.com/a/b",
	}
	for in, want := range good {
		r, err := Parse(in)
		if err != nil || r.String() != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, r.String(), err, want)
		}
	}
	if r, _ := Parse("a/b"); r.URL() != "https://github.com/a/b.git" {
		t.Errorf("URL %q", r.URL())
	}
	bad := []string{
		"", "golang", "file:///etc", "/etc/passwd", "../x/y", "git@github.com:a/b.git",
		"ssh://github.com/a/b", "http://github.com/a/b", "ext::sh -c id", "-u/x",
		"https://user:pw@github.com/a/b", "https://localhost/a/b", "https://127.0.0.1/a/b",
		"https://10.0.0.5/a/b", "https://git.internal/a/b", "https://box.local/a/b", "https://intranet/a/b",
		"https://github.com/-a/b", "https://github.com/a/..", "https://github.com/a/b c", "https://[::1]/a/b",
		"https://github.com:22/a/b",
	}
	for _, in := range bad {
		if r, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %q, want an error", in, r.String())
		}
	}
}

// CMP-91: a ref is a plain branch or tag name.
func TestCheckRef(t *testing.T) {
	for _, ok := range []string{"", "main", "v1.2.3", "release/2026-11"} {
		if err := CheckRef(ok); err != nil {
			t.Errorf("CheckRef(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"-x", "a..b", "a b", "--upload-pack=x", strings.Repeat("a", 101), "a;b", "~1"} {
		if CheckRef(bad) == nil {
			t.Errorf("CheckRef(%q) passed", bad)
		}
	}
}

// fakeGit stands in for git: clone writes the files into the target
// folder (the last argument), rev-parse prints a hash.
type fakeGit struct {
	calls []string
	files map[string]string
	err   error
}

func (f *fakeGit) run(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.err != nil {
		return "", f.err
	}
	if len(args) > 0 && args[0] == "rev-parse" {
		return "0123456789abcdef0123456789abcdef01234567\n", nil
	}
	target := args[len(args)-1]
	for rel, body := range f.files {
		p := filepath.Join(target, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	return "", nil
}

// CMP-91: Fetch clones once (shallow, no prompts, no hooks, https only,
// links as plain files), drops .git and reuses the copy after; a ref is
// its own copy. A failed clone leaves nothing behind.
func TestFetch(t *testing.T) {
	g := &fakeGit{files: map[string]string{"README.md": "hello", "src/a.go": "package a", ".git/config": "x"}}
	c := Cache{Root: t.TempDir(), Git: g.run}
	r, _ := Parse("a/b")
	got, err := c.Fetch(context.Background(), r, "")
	if err != nil || got.Cached || got.Commit == "" {
		t.Fatalf("fetch: %+v %v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(got.Dir, "src", "a.go")); err != nil || string(b) != "package a" {
		t.Errorf("file %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, ".git")); !os.IsNotExist(err) {
		t.Errorf(".git kept: %v", err)
	}
	clone := g.calls[0]
	for _, want := range []string{"clone", "--depth 1", "--single-branch", "core.symlinks=false", "protocol.allow=never", "protocol.https.allow=always", "core.hooksPath=", "https://github.com/a/b.git"} {
		if !strings.Contains(clone, want) {
			t.Errorf("clone %q has no %q", clone, want)
		}
	}
	if !strings.Contains(clone, "-- https://") {
		t.Errorf("no -- before the URL: %q", clone)
	}
	n := len(g.calls)
	again, err := c.Fetch(context.Background(), r, "")
	if err != nil || !again.Cached || again.Dir != got.Dir || len(g.calls) != n {
		t.Errorf("second fetch cloned again: %+v %v calls %d", again, err, len(g.calls))
	}
	if dir, ok := c.Have(r, ""); !ok || dir != got.Dir {
		t.Errorf("Have %q %v", dir, ok)
	}
	if _, ok := c.Have(r, "v1"); ok {
		t.Error("another ref is not fetched yet")
	}
	tagged, err := c.Fetch(context.Background(), r, "v1")
	if err != nil || tagged.Dir == got.Dir || !strings.Contains(g.calls[len(g.calls)-2], "--branch v1") {
		t.Errorf("ref fetch %+v %v %v", tagged, err, g.calls)
	}

	bad := &fakeGit{err: errors.New("fatal: repository not found")}
	c2 := Cache{Root: t.TempDir(), Git: bad.run}
	r2, _ := Parse("a/missing")
	if _, err := c2.Fetch(context.Background(), r2, ""); err == nil {
		t.Fatal("a failed clone passed")
	}
	if _, ok := c2.Have(r2, ""); ok {
		t.Error("a failed clone is cached")
	}
	left := 0
	_ = filepath.Walk(c2.Root, func(p string, fi os.FileInfo, _ error) error {
		if fi != nil && !fi.IsDir() {
			left++
		}
		return nil
	})
	if left != 0 {
		t.Errorf("%d files left after a failed clone", left)
	}
}

// CMP-91: a repository over the size cap is refused and removed.
func TestFetchTooBig(t *testing.T) {
	g := &fakeGit{files: map[string]string{"big.bin": strings.Repeat("x", 2048)}}
	c := Cache{Root: t.TempDir(), Git: g.run, MaxBytes: 1024}
	r, _ := Parse("a/big")
	if _, err := c.Fetch(context.Background(), r, ""); err == nil || !strings.Contains(err.Error(), "too big") {
		t.Fatalf("err %v", err)
	}
	if _, ok := c.Have(r, ""); ok {
		t.Error("a too big repository is cached")
	}
}

// CMP-91: copies older than the TTL go on the next fetch.
func TestPrune(t *testing.T) {
	g := &fakeGit{files: map[string]string{"a.txt": "a"}}
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	c := Cache{Root: t.TempDir(), Git: g.run, Now: func() time.Time { return now }, TTL: time.Hour}
	r, _ := Parse("a/old")
	got, err := c.Fetch(context.Background(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-2 * time.Hour)
	_ = os.Chtimes(filepath.Join(got.Dir, markerFile), old, old)
	if n := c.Prune(); n != 1 {
		t.Errorf("pruned %d", n)
	}
	if _, ok := c.Have(r, ""); ok {
		t.Error("an old copy stayed")
	}
}
