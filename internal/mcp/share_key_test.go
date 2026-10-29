//go:build unix

package mcp

import (
	"context"
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
}

// longTMPDIR makes $TMPDIR as long as macOS's /var/folders/…/T/.
func longTMPDIR(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "var", "folders", "n6", strings.Repeat("x", 30), "T")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", d)
	return d
}

// cleanShareDir removes the short socket folder of a test.
func cleanShareDir(t *testing.T, dir string) {
	t.Cleanup(func() { cleanShortDir(dir) })
}

// cleanShortDir removes the short /tmp folder that dir maps to, if any.
func cleanShortDir(dir string) {
	if short := ShareSocketDir(dir); strings.HasPrefix(short, shortShareBase+"/termocode-") {
		_ = os.RemoveAll(short)
	}
}

// Spec: a socket folder too long for a unix socket (macOS $TMPDIR) gets
// a short private folder instead; sharing still works.
func TestShareDirTooLongGetsShortFolder(t *testing.T) {
	longTMPDIR(t)
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(os.TempDir(), "cache"))
	for name, o := range map[string]*ShareOptions{
		"given":   {Dir: filepath.Join(os.TempDir(), "mcp")},
		"default": {},
	} {
		got, err := shareDir(o)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cleanShareDir(t, got)
		if len(got)+1+32+len(".sock") > maxShareSocket {
			t.Fatalf("%s: %s is still too long", name, got)
		}
		fi, err := os.Lstat(got)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %s: %v %v", name, got, fi, err)
		}
		again, _ := shareDir(o)
		if again != got {
			t.Fatalf("%s: other folder the second time: %s, %s", name, got, again)
		}
	}
	a, _ := shareDir(&ShareOptions{Dir: filepath.Join(os.TempDir(), "a")})
	b, _ := shareDir(&ShareOptions{Dir: filepath.Join(os.TempDir(), "b")})
	cleanShareDir(t, a)
	cleanShareDir(t, b)
	if a == b {
		t.Fatal("two long folders share one short folder")
	}
}

// Spec: the short folder keeps the checks: another user's or a loose
// one is refused (sharing then falls back).
func TestShareShortFolderKeepsChecks(t *testing.T) {
	longTMPDIR(t)
	o := &ShareOptions{Dir: filepath.Join(os.TempDir(), "mcp")}
	got, err := shareDir(o)
	if err != nil {
		t.Fatal(err)
	}
	cleanShareDir(t, got)
	if err := os.Chmod(got, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := shareDir(o); err == nil {
		t.Fatal("a 0755 short folder was accepted")
	}
}

func TestShareBrokerWithLongTMPDIR(t *testing.T) {
	longTMPDIR(t)
	_, sock := startTestBroker(t, fakeShareConfig(nil))
	if len(sock) > maxShareSocket {
		t.Fatalf("socket %s", sock)
	}
	c := dialTestShare(t, sock, nil)
	if res, err := c.CallTool(context.Background(), "echo", map[string]any{"text": "x"}); err != nil || res.Text() != "echo: x" {
		t.Fatalf("echo %q %v", res.Text(), err)
	}
}
