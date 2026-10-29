//go:build unix

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShareKeySameConfigSameKey(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	a := shareConfig{Command: "npx", Args: []string{"-y", "srv"}, Env: map[string]string{"A": "1", "TOKEN": "s3cret"}, Cwd: "/w", Roots: []string{"/w"}, Source: SourceUser}
	b := a
	b.Env = map[string]string{"TOKEN": "s3cret", "A": "1"}
	b.GraceMS = 5 // not part of the key
	if shareKey(salt, a) != shareKey(salt, b) {
		t.Fatal("same config, other key")
	}
	for name, c := range map[string]shareConfig{
		"env value": {Command: "npx", Args: a.Args, Env: map[string]string{"A": "1", "TOKEN": "other"}, Cwd: "/w", Roots: a.Roots, Source: SourceUser},
		"args":      {Command: "npx", Args: []string{"srv"}, Env: a.Env, Cwd: "/w", Roots: a.Roots, Source: SourceUser},
		"cwd":       {Command: "npx", Args: a.Args, Env: a.Env, Cwd: "/x", Roots: a.Roots, Source: SourceUser},
		"roots":     {Command: "npx", Args: a.Args, Env: a.Env, Cwd: "/w", Roots: []string{"/x"}, Source: SourceUser},
		"source":    {Command: "npx", Args: a.Args, Env: a.Env, Cwd: "/w", Roots: a.Roots, Source: SourceProject},
	} {
		if shareKey(salt, c) == shareKey(salt, a) {
			t.Errorf("%s changed, same key", name)
		}
	}
	if shareKey([]byte("another salt, another user......"), a) == shareKey(salt, a) {
		t.Error("the salt is not part of the key")
	}
	if k := shareKey(salt, a); len(k) != 32 || strings.Contains(k, "s3cret") {
		t.Errorf("key %q", k)
	}
}

func TestShareDirIsPrivateAndSaltIsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mcp")
	got, err := shareDir(&ShareOptions{Dir: dir})
	if err != nil || got != dir {
		t.Fatalf("shareDir = %q, %v", got, err)
	}
	fi, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", fi.Mode().Perm())
	}
	s1, err := shareSalt(dir)
	if err != nil || len(s1) != 32 {
		t.Fatalf("salt %v", err)
	}
	fi, _ = os.Stat(filepath.Join(dir, "salt"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("salt mode %o", fi.Mode().Perm())
	}
	s2, _ := shareSalt(dir)
	if string(s1) != string(s2) {
		t.Fatal("a second session got another salt")
	}
}

func TestShareDirRefusesLooseFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mcp")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := shareDir(&ShareOptions{Dir: dir}); err == nil {
		t.Fatal("a 0755 folder was accepted")
	}
	link := filepath.Join(t.TempDir(), "link")
	_ = os.Symlink(t.TempDir(), link)
	if _, err := shareDir(&ShareOptions{Dir: link}); err == nil {
		t.Fatal("a symlink was accepted")
	}
	long := filepath.Join(t.TempDir(), strings.Repeat("x", 90))
	if _, err := shareDir(&ShareOptions{Dir: long}); err == nil {
		t.Fatal("a too long folder was accepted")
	}
}
