//go:build unix

package mcp

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// changingSource is a source whose tool list can change (MS-7).
type changingSource struct{ serverTestSource }

func (changingSource) ToolsCanChange() bool { return true }

// listChangedClient is one socket client with its own reader.
type listChangedClient struct {
	c net.Conn
	r *bufio.Reader
}

func dialListChanged(t *testing.T, path string) *listChangedClient {
	t.Helper()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &listChangedClient{c: c, r: bufio.NewReader(c)}
}

func (l *listChangedClient) send(t *testing.T, line string) {
	t.Helper()
	if _, err := l.c.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

// read returns the next line, or "" when none comes within wait.
func (l *listChangedClient) read(wait time.Duration) string {
	_ = l.c.SetReadDeadline(time.Now().Add(wait))
	line, _ := l.r.ReadString('\n')
	return line
}

// initialized sends notifications/initialized, then a ping whose answer
// shows the server has read it (one connection reads in order).
func (l *listChangedClient) initialized(t *testing.T) {
	t.Helper()
	l.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	l.send(t, `{"jsonrpc":"2.0","id":99,"method":"ping"}`)
	if got := l.read(5 * time.Second); !strings.Contains(got, `"id":99`) {
		t.Fatalf("ping: %q", got)
	}
}

const listChangedInit = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"a"}}}`

// Spec (MS-7): a source whose tools can change advertises
// tools.listChanged; NotifyToolsChanged sends
// notifications/tools/list_changed to every client that finished
// initialize (notifications/initialized) or spoke the stateless revision,
// and to no other.
func TestSocketServerNotifyToolsChanged(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	src := changingSource{echoSource().(serverTestSource)}
	srv := Serve(ln, src, func(net.Conn) error { return nil }, nil)
	defer srv.Close()

	a := dialListChanged(t, path)
	a.send(t, listChangedInit)
	if got := a.read(5 * time.Second); !strings.Contains(got, `"tools":{"listChanged":true}`) {
		t.Fatalf("initialize: %q", got)
	}
	a.initialized(t)
	halfway := dialListChanged(t, path) // initialize answered, initialized not sent yet
	halfway.send(t, listChangedInit)
	if got := halfway.read(5 * time.Second); !strings.Contains(got, `"listChanged":true`) {
		t.Fatalf("initialize: %q", got)
	}
	stateless := dialListChanged(t, path)
	stateless.send(t, `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"s"}}}}`)
	if got := stateless.read(5 * time.Second); !strings.Contains(got, `"echo"`) {
		t.Fatalf("stateless tools/list: %q", got)
	}
	silent := dialListChanged(t, path) // connected, never initialized
	deadline := time.Now().Add(5 * time.Second)
	for srv.Conns() != 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	srv.NotifyToolsChanged()
	for name, c := range map[string]*listChangedClient{"initialized": a, "stateless": stateless} {
		got := c.read(5 * time.Second)
		if !strings.Contains(got, `"method":"notifications/tools/list_changed"`) || strings.Contains(got, `"id"`) {
			t.Errorf("%s client got %q", name, got)
		}
	}
	for name, c := range map[string]*listChangedClient{"silent": silent, "halfway": halfway} {
		if got := c.read(200 * time.Millisecond); got != "" {
			t.Fatalf("%s client (not initialized) got %q", name, got)
		}
	}
}

// Spec (MS-7): a source whose tools never change says listChanged false
// and NotifyToolsChanged sends nothing.
func TestSocketServerNoListChangedForFixedSource(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := Serve(ln, echoSource(), func(net.Conn) error { return nil }, nil)
	defer srv.Close()
	a := dialListChanged(t, path)
	a.send(t, listChangedInit)
	if got := a.read(5 * time.Second); !strings.Contains(got, `"tools":{"listChanged":false}`) {
		t.Fatalf("initialize: %q", got)
	}
	a.initialized(t)
	srv.NotifyToolsChanged()
	if got := a.read(200 * time.Millisecond); got != "" {
		t.Fatalf("got %q", got)
	}
}
