//go:build unix

package session

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (MS-1): the MCP socket sits next to the session socket, under the
// same key, short enough for sun_path.
func TestPathsMCPSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortTemp(t))
	p, err := PathsFor("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	if p.MCPSocket != strings.TrimSuffix(p.Socket, ".sock")+".mcp.sock" || filepath.Base(p.MCPSocket) != p.Key+".mcp.sock" {
		t.Fatalf("MCPSocket = %q (socket %q)", p.MCPSocket, p.Socket)
	}
	if len(p.MCPSocket) > maxSocketPath {
		t.Fatalf("MCP socket path too long: %d", len(p.MCPSocket))
	}
}

// Spec (MS-1): a long runtime dir that still fits the session socket but
// not the MCP socket falls back to the short dir.
func TestDirFitsMCPSocket(t *testing.T) {
	long := "/" + strings.Repeat("y", maxSocketPath-len("/termocode")-len("/0123456789abcdef.sock")-1)
	t.Setenv("XDG_RUNTIME_DIR", long)
	t.Setenv("TMPDIR", shortTemp(t))
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, long) || len(dir)+len("/0123456789abcdef.mcp.sock") > maxSocketPath {
		t.Fatalf("dir %q does not fit the MCP socket", dir)
	}
}

// Spec (MS-1): CheckPeer takes a same-uid unix connection and refuses
// anything that is not a unix socket.
func TestCheckPeerExported(t *testing.T) {
	sock := filepath.Join(shortTemp(t), "p.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := net.Dial("unix", sock)
		if err == nil {
			defer c.Close()
			_, _ = c.Read(make([]byte, 1))
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := CheckPeer(c); err != nil {
		t.Errorf("own uid refused: %v", err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if err := CheckPeer(a); err == nil {
		t.Error("a pipe passed the peer check")
	}
}
