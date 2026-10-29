//go:build unix

package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec (G-MCP): the socket folder must be a real, private folder; a
// path under a file or a file in its place is refused.
func TestShareDirRefusesFiles(t *testing.T) {
	base := shortTemp(t)
	file := filepath.Join(base, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := shareDir(&ShareOptions{Dir: filepath.Join(file, "sub", "mcp")}); err == nil {
		t.Fatal("a folder under a file was accepted")
	}
	if _, err := shareDir(&ShareOptions{Dir: file}); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Fatalf("a file as the folder: err = %v", err)
	}
}

// Spec (G-MCP): a damaged salt is refused (never replaced, so keys stay
// stable); a leftover temp file of a crashed session does not block.
func TestShareSaltEdgeCases(t *testing.T) {
	dir, err := shareDir(&ShareOptions{Dir: filepath.Join(shortTemp(t), "mcp")})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "salt")
	if err := os.WriteFile(p, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := shareSalt(dir); err == nil || !strings.Contains(err.Error(), "bad salt") {
		t.Fatalf("short salt: err = %v", err)
	}
	if got := readFileString(t, p); got != "short" {
		t.Fatal("the damaged salt was replaced")
	}

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := shareSalt(dir); err == nil {
		t.Fatal("a salt that is a folder was accepted")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p+".tmp", []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	salt, err := shareSalt(dir)
	if err != nil || len(salt) != 32 {
		t.Fatalf("after a leftover temp file: %d bytes, %v", len(salt), err)
	}
	if _, err := os.Stat(p + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the temp file stayed")
	}
	again, err := shareSalt(dir)
	if err != nil || string(again) != string(salt) {
		t.Fatal("the salt changed between calls")
	}
}

func readFileString(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Spec (G-MCP): a broker that can not run, says it is unavailable or
// answers nonsense makes the session run its own process
// (errShareUnavailable); "error: …" is the server's own error.
func TestSpawnBrokerAnswers(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "x.sock")
	sh := func(script string) *ShareOptions {
		return &ShareOptions{Exe: "/bin/sh", Args: []string{"-c", script}}
	}
	cases := map[string]struct {
		o           *ShareOptions
		unavailable bool
		want        string
	}{
		"no program":  {&ShareOptions{}, true, "no broker program"},
		"not found":   {&ShareOptions{Exe: filepath.Join(t.TempDir(), "missing")}, true, ""},
		"unavailable": {sh("read x; echo 'unavailable: no sockets here'"), true, "no sockets here"},
		"nonsense":    {sh("read x; echo hello"), true, "did not start"},
		"silent exit": {sh("read x; exit 0"), true, "did not start"},
		"server err":  {sh("read x; echo 'error: npx: not found'"), false, "npx: not found"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := spawnBroker(callCtx(t), c.o, sock, shareConfig{Command: "x"})
			if err == nil || errors.Is(err, errShareUnavailable) != c.unavailable || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v (unavailable %v), want %q", err, errors.Is(err, errShareUnavailable), c.want)
			}
		})
	}
}

// Spec (G-MCP): a broker that never answers is given up when the
// session's context ends.
func TestSpawnBrokerNoAnswer(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "x.sock")
	c, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := spawnBroker(c, &ShareOptions{Exe: "/bin/sh", Args: []string{"-c", "read x; exec sleep 30"}}, sock, shareConfig{})
	if !errors.Is(err, errShareUnavailable) || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("gave up after %v", d)
	}
}

// Spec (G-MCP): while another session holds the start lock, a session
// whose context ends stops waiting.
func TestLockShareWaitEndsWithContext(t *testing.T) {
	p := filepath.Join(shortTemp(t), "k.lock")
	unlock, err := lockShare(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	c, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := lockShare(c, p); err == nil || !strings.Contains(err.Error(), "lock k.lock") {
		t.Fatalf("err = %v", err)
	}
	if _, err := lockShare(context.Background(), filepath.Join(p, "under-a-file")); err == nil {
		t.Fatal("a lock path under a file was accepted")
	}
}

// Spec (G-MCP): a broker that reports the server's own error makes the
// start fail with it (no own process is tried).
func TestStartSharedServerError(t *testing.T) {
	o := &ShareOptions{Exe: "/bin/sh", Args: []string{"-c", "read x; echo 'error: the server would not start'"},
		Dir: filepath.Join(shortTemp(t), "mcp")}
	_, err := startShared(callCtx(t), o, Server{Name: "x", Command: "x", Cwd: "/"}, nil, Options{})
	if err == nil || errors.Is(err, errShareUnavailable) || err.Error() != "the server would not start" {
		t.Fatalf("err = %v", err)
	}
	o.Dir = filepath.Join(shortTemp(t), "f")
	if err := os.WriteFile(o.Dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startShared(callCtx(t), o, Server{Name: "x", Command: "x"}, nil, Options{}); !errors.Is(err, errShareUnavailable) {
		t.Fatalf("bad folder: err = %v", err)
	}
}
