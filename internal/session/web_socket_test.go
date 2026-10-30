//go:build unix

package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WEB-6): the web hub socket and the gateway's HTTP socket sit next
// to the session socket under the same key, and Dir keeps the longest of
// them inside sun_path: a runtime dir that fits ".mcp.sock" but not
// ".webhttp.sock" falls back to the short dir.
func TestWebSocketNamesFit(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", shortTemp(t))
	p, err := PathsFor("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSuffix(p.Socket, ".sock")
	if p.WebSocket != base+".web.sock" || filepath.Base(p.WebSocket) != p.Key+".web.sock" {
		t.Fatalf("WebSocket = %q (socket %q)", p.WebSocket, p.Socket)
	}
	if p.WebHTTPSocket != base+".webhttp.sock" {
		t.Fatalf("WebHTTPSocket = %q (socket %q)", p.WebHTTPSocket, p.Socket)
	}
	for _, s := range []string{p.WebSocket, p.WebHTTPSocket} {
		if len(s) > maxSocketPath {
			t.Fatalf("%s is %d bytes, over %d", s, len(s), maxSocketPath)
		}
	}

	// Fits the MCP socket exactly, one byte too short for the longest web name.
	long := "/" + strings.Repeat("w", maxSocketPath-len("/termocode")-len("/0123456789abcdef.mcp.sock")-1)
	t.Setenv("XDG_RUNTIME_DIR", long)
	t.Setenv("TMPDIR", shortTemp(t))
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(dir, long) || len(dir)+len("/0123456789abcdef.webhttp.sock") > maxSocketPath {
		t.Fatalf("dir %q does not fit the web sockets", dir)
	}
}
