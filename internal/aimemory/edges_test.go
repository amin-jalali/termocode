package aimemory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Word matches rank by how many words they share, then newest first; the
// count cap stops the list.
func TestSelectRanksWordMatches(t *testing.T) {
	ls := []Lesson{
		lesson("2026-10-01", "Cache keys", "cache keys go stale"),                         // 0: 2 words
		lesson("2026-10-02", "Cache keys again", "cache keys expire early during deploy"), // 1: 3 words
		lesson("2026-10-03", "Keys cache", "cache keys"),                                  // 2: 2 words, newer than 0
	}
	h := Hints{Text: "why do cache keys expire?"}
	if got := titles(Select(ls, h, 2<<10, 10)); !reflect.DeepEqual(got, []string{"Cache keys again", "Keys cache", "Cache keys"}) {
		t.Fatalf("order %v", got)
	}
	if got := titles(Select(ls, h, 2<<10, 2)); !reflect.DeepEqual(got, []string{"Cache keys again", "Keys cache"}) {
		t.Fatalf("capped %v", got)
	}
}

// An empty or "./" lesson path matches no file.
func TestSelectSkipsEmptyLessonPaths(t *testing.T) {
	ls := []Lesson{lesson("2026-10-01", "Blank path", "x", "./"), lesson("2026-10-02", "Real", "y", "a.go")}
	h := Hints{Paths: []string{"/repo/b.go"}}
	if got := titles(Select(ls, h, 2<<10, 1)); !reflect.DeepEqual(got, []string{"Real"}) {
		t.Fatalf("got %v (newest fill, no path hit)", got)
	}
}

func TestLoadSaveErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A workspace path that is a file: not "no lessons", an error.
	if _, err := Load(file); err == nil {
		t.Error("load under a file must fail")
	}
	if err := Save(file, nil); err == nil {
		t.Error("save under a file must fail")
	}
	// The lessons file is a folder: the rename fails and no temp file stays.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(Path(root), "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, []Lesson{lesson("2026-10-01", "T", "x")}); err == nil {
		t.Error("save over a folder must fail")
	}
	ents, _ := os.ReadDir(Dir(root))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".lessons-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestLoadSavePermissionErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	root := t.TempDir()
	if err := Save(root, []Lesson{lesson("2026-10-01", "T", "x")}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(Path(root), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); !os.IsPermission(err) {
		t.Errorf("unreadable file: %v", err)
	}
	// A read-only .termocode: the memory folder cannot be made.
	ro := t.TempDir()
	tc := filepath.Join(ro, ".termocode")
	if err := os.Mkdir(tc, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tc, 0o755) })
	if err := Save(ro, nil); !os.IsPermission(err) {
		t.Errorf("read-only .termocode: %v", err)
	}
	// A read-only memory folder: no temp file can be made.
	if err := os.Chmod(tc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(Dir(ro), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(Dir(ro), 0o755) })
	if err := Save(ro, nil); !os.IsPermission(err) {
		t.Errorf("read-only memory folder: %v", err)
	}
}

func TestEnsureIgnoredErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIgnored(root); err == nil {
		t.Error("a .gitignore folder must fail")
	}
	if os.Geteuid() == 0 {
		return
	}
	ro := t.TempDir()
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := EnsureIgnored(ro); !os.IsPermission(err) {
		t.Errorf("read-only workspace: %v", err)
	}
	locked := t.TempDir()
	gi := filepath.Join(locked, ".gitignore")
	if err := os.WriteFile(gi, []byte("node_modules/\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIgnored(locked); !os.IsPermission(err) {
		t.Errorf("read-only .gitignore: %v", err)
	}
	if b, _ := os.ReadFile(gi); string(b) != "node_modules/\n" {
		t.Errorf(".gitignore changed: %q", b)
	}
}
