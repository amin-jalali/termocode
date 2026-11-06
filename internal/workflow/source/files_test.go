package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, root, rel, text string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func keys(r Result) string {
	var ks []string
	for _, it := range r.Items {
		ks = append(ks, it.Key)
	}
	return strings.Join(ks, " ")
}

func TestFilesGlob(t *testing.T) {
	ws := t.TempDir()
	for _, f := range []string{"README.md", "docs/a.md", "docs/x/b.md", "docs/x/c.txt", ".git/HEAD.md", "src/d.md"} {
		writeFile(t, ws, f, "hi "+f)
	}
	s := &Sources{Workspace: ws}
	cases := map[string]string{
		"docs/**/*.md": "docs/a.md docs/x/b.md",
		"**/*.md":      "README.md docs/a.md docs/x/b.md src/d.md",
		"docs/*":       "docs/a.md",
		"*.md":         "README.md",
		"./docs/x/*":   "docs/x/b.md docs/x/c.txt",
		"nothing/*":    "",
	}
	for g, want := range cases {
		r, err := s.Files(context.Background(), FilesInput{Glob: g})
		if err != nil {
			t.Fatalf("%q: %v", g, err)
		}
		if got := keys(r); got != want {
			t.Errorf("%q: got %q, want %q", g, got, want)
		}
	}
	r, err := s.Files(context.Background(), FilesInput{Glob: "docs/a.md", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	var it fileItem
	if err := json.Unmarshal(r.Items[0].Data, &it); err != nil || it.Content != "hi docs/a.md" || it.Path != "docs/a.md" || it.Size != 12 {
		t.Errorf("item: %+v %v", it, err)
	}
}

func TestFilesNeverLeaveWorkspace(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	writeFile(t, outside, "secret.md", "outside")
	writeFile(t, outside, "dir/in.md", "outside dir")
	writeFile(t, ws, "ok.md", "inside")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(ws, "leak.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(ws, "leakdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "ok.md"), filepath.Join(ws, "alias.md")); err != nil {
		t.Fatal(err)
	}
	s := &Sources{Workspace: ws}
	r, err := s.Files(context.Background(), FilesInput{Glob: "**/*.md", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := keys(r); got != "alias.md ok.md" {
		t.Errorf("got %q, want only files inside", got)
	}
	for _, it := range r.Items {
		if strings.Contains(string(it.Data), "outside") {
			t.Errorf("read a file outside: %s", it.Data)
		}
	}
	for _, g := range []string{"../*", "docs/../../x", "/etc/*", outside + "/*", `docs\*.md`, "", "[", "**/**/**/**/*"} {
		_, err := s.Files(context.Background(), FilesInput{Glob: g})
		if err == nil {
			t.Errorf("%q: want an error", g)
		}
		if strings.Contains(g, "..") || strings.HasPrefix(g, "/") {
			if !errors.Is(err, ErrOutside) {
				t.Errorf("%q: want ErrOutside, got %v", g, err)
			}
		}
	}
}

func TestFilesCapsAndChangedSince(t *testing.T) {
	ws := t.TempDir()
	for _, f := range []string{"a.md", "b.md", "c.md"} {
		writeFile(t, ws, f, "x")
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(ws, "a.md"), old, old); err != nil {
		t.Fatal(err)
	}
	s := &Sources{Workspace: ws}
	r, err := s.Files(context.Background(), FilesInput{Glob: "*.md", MaxItems: 2})
	if err != nil || len(r.Items) != 2 || !r.Truncated || r.Total != 3 {
		t.Errorf("cap: %+v %v", r, err)
	}
	r, err = s.Files(context.Background(), FilesInput{Glob: "*.md", ChangedSince: time.Now().Add(-time.Minute)})
	if err != nil || keys(r) != "b.md c.md" {
		t.Errorf("changed since: %q %v", keys(r), err)
	}
	writeFile(t, ws, "big.md", strings.Repeat("é", MaxItemBytes))
	writeFile(t, ws, "bin.md", "a\x00b")
	r, err = s.Files(context.Background(), FilesInput{Glob: "b*.md", Content: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range r.Items {
		if len(it.Data) > MaxItemBytes {
			t.Errorf("%s: %d bytes", it.Key, len(it.Data))
		}
		var fi fileItem
		_ = json.Unmarshal(it.Data, &fi)
		switch it.Key {
		case "big.md":
			if !fi.Truncated || fi.Content == "" || !strings.HasPrefix(fi.Content, "éé") {
				t.Errorf("big: truncated=%v len=%d", fi.Truncated, len(fi.Content))
			}
		case "bin.md":
			if !fi.Binary || fi.Content != "" {
				t.Errorf("bin: %+v", fi)
			}
		}
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		g, name string
		want    bool
	}{
		{"**", "a/b/c", true},
		{"a/**", "a", true},
		{"a/**/c", "a/c", true},
		{"a/**/c", "a/b/x/c", true},
		{"a/**/c", "a/b/x/d", false},
		{"*.go", "a/b.go", false},
		{"**/*.go", "b.go", true},
	}
	for _, c := range cases {
		pat, err := cleanGlob(c.g)
		if err != nil {
			t.Fatal(err)
		}
		if got := matchGlob(pat, c.name); got != c.want {
			t.Errorf("%q ~ %q: %v", c.g, c.name, got)
		}
	}
}
