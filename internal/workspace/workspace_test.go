package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
)

func TestLocalID(t *testing.T) {
	if got := LocalID("/home/u/proj/"); got != "local:/home/u/proj" {
		t.Fatalf("LocalID = %q", got)
	}
	kind, rest := ParseID("local:/home/u/proj")
	if kind != KindLocal || rest != "/home/u/proj" {
		t.Fatalf("ParseID = %q %q", kind, rest)
	}
	if kind, _ := ParseID("container:prod/api-1:/app"); kind != KindContainer {
		t.Fatalf("container kind = %q", kind)
	}
	if kind, _ := ParseID("nonsense"); kind != "" {
		t.Fatalf("unknown kind = %q", kind)
	}
}

// oldKey is the formula sessions, hot-exit backups and per-workspace
// session files were stored under before workspace IDs existed.
func oldKey(path string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return hex.EncodeToString(sum[:8])
}

func TestKeyOfLocalIDMatchesOldPathKey(t *testing.T) {
	for _, p := range []string{"/home/u/proj", "/home/u/proj/", "/tmp/a/../b"} {
		if got, want := Key(LocalID(p)), oldKey(p); got != want {
			t.Fatalf("Key(LocalID(%q)) = %s, want the old key %s", p, got, want)
		}
	}
}

func TestKeyOfOtherKindsDiffers(t *testing.T) {
	a := Key("container:prod/api-1:/app")
	b := Key("container:prod/api-2:/app")
	if a == b || len(a) != 16 {
		t.Fatalf("container keys %q %q", a, b)
	}
	if a == oldKey("/app") {
		t.Fatal("a container key must not collide with the local key of its path")
	}
}

func TestResolve(t *testing.T) {
	root := "/w/proj"
	ok := map[string]string{
		"":                 root,
		".":                root,
		"a.go":             "/w/proj/a.go",
		"sub/../b.go":      "/w/proj/b.go",
		"/w/proj/c/d.go":   "/w/proj/c/d.go",
		"/w/proj":          root,
		"./x/./y":          "/w/proj/x/y",
		"..proj-like-name": "/w/proj/..proj-like-name",
	}
	for in, want := range ok {
		got, err := Resolve(root, in)
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../x", "a/../../x", "/w/projX/a", "/etc/passwd", "/w"} {
		if got, err := Resolve(root, in); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("Resolve(%q) = %q, %v; want ErrOutsideRoot", in, got, err)
		}
	}
}

func TestResolveNeedsAbsoluteRoot(t *testing.T) {
	if _, err := Resolve("rel/root", "a"); err == nil {
		t.Fatal("a relative root must be refused")
	}
}

func TestWorkspaceZero(t *testing.T) {
	var w Workspace
	if !w.IsZero() {
		t.Fatal("zero workspace")
	}
	if (Workspace{Root: "/x"}).IsZero() {
		t.Fatal("non-zero workspace")
	}
}
