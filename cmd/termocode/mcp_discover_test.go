//go:build unix

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// shortTemp is a short folder: unix socket paths are limited to 104 bytes
// on macOS.
func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "tc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	d, _ = filepath.EvalSymlinks(d)
	return d
}

// sessionEnv points the session dir at a private short folder.
func sessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", shortTemp(t))
}

// listenMCP makes a socket file at the MCP socket path of ws.
func listenMCP(t *testing.T, ws string) string {
	t.Helper()
	paths, err := mcpPathsFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return paths
}

func TestFindMCPSocketExplicitDir(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	sock := listenMCP(t, ws)
	got, err := findMCPSocket(ws, true, "/nowhere")
	if err != nil || got.Socket != sock || got.Workspace != ws {
		t.Fatalf("got %+v, %v; want socket %s", got, err, sock)
	}
}

func TestFindMCPSocketExplicitDirDoesNotWalkUp(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	listenMCP(t, ws)
	sub := filepath.Join(ws, "a")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findMCPSocket(sub, true, ws)
	if err != nil || got.Socket != "" || got.Workspace != sub {
		t.Fatalf("explicit --dir must not walk up: %+v, %v", got, err)
	}
}

func TestFindMCPSocketWalksUp(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	sock := listenMCP(t, ws)
	deep := filepath.Join(ws, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findMCPSocket("", false, deep)
	if err != nil || got.Socket != sock || got.Workspace != ws {
		t.Fatalf("got %+v, %v; want %s in %s", got, err, sock, ws)
	}
}

func TestFindMCPSocketNearestWins(t *testing.T) {
	sessionEnv(t)
	outer := shortTemp(t)
	inner := filepath.Join(outer, "in")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	listenMCP(t, outer)
	sock := listenMCP(t, inner)
	got, _ := findMCPSocket("", false, inner)
	if got.Socket != sock {
		t.Fatalf("nearest folder must win: %+v", got)
	}
}

func TestFindMCPSocketMaxDepth(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	listenMCP(t, ws)
	deep := ws
	for i := 0; i < mcpWalkLevels+1; i++ {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, _ := findMCPSocket("", false, deep)
	if got.Socket != "" {
		t.Fatalf("walk-up must stop after %d levels: %+v", mcpWalkLevels, got)
	}
}

// Not found: no socket, and the workspace to name in the message is the
// start folder.
func TestFindMCPSocketNotFound(t *testing.T) {
	sessionEnv(t)
	start := shortTemp(t)
	got, err := findMCPSocket("", false, start)
	if err != nil || got.Socket != "" || got.Workspace != start {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// A plain file at the socket path is not a socket.
func TestFindMCPSocketIgnoresPlainFile(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	p, err := mcpPathsFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := findMCPSocket(ws, true, "")
	if got.Socket != "" {
		t.Fatalf("a plain file is not a socket: %+v", got)
	}
}

func TestFindMCPSocketRelativeDir(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	sock := listenMCP(t, ws)
	t.Chdir(filepath.Dir(ws))
	got, err := findMCPSocket(filepath.Base(ws), true, "")
	if err != nil || got.Socket != sock {
		t.Fatalf("got %+v, %v", got, err)
	}
}
