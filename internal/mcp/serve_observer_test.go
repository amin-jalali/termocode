//go:build unix

package mcp

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// observedSource is a ToolSource that also hears the clients' life.
type observedSource struct {
	ToolSource
	mu     sync.Mutex
	events []string
	ids    map[string]uint64
	calls  chan uint64
}

func (o *observedSource) note(e string, id uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, e)
	o.ids[e] = id
}

func (o *observedSource) ClientConnected(id uint64)          { o.note("connected", id) }
func (o *observedSource) ClientNamed(id uint64, name string) { o.note("named:"+name, id) }
func (o *observedSource) ClientGone(id uint64)               { o.note("gone", id) }

func (o *observedSource) Call(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	o.calls <- ConnID(ctx)
	return o.ToolSource.Call(ctx, name, args)
}

func (o *observedSource) seen() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.events...)
}

// Spec (MS-3): a source that implements ClientObserver hears connect,
// the name from initialize and the end of each connection, with one id per
// connection that its calls also see (ConnID).
func TestSocketServerObservesClients(t *testing.T) {
	src := &observedSource{ToolSource: echoSource(), ids: map[string]uint64{}, calls: make(chan uint64, 4)}
	path := filepath.Join(shortSocketDir(t), "k.mcp.sock")
	ln, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := Serve(ln, src, func(net.Conn) error { return nil }, nil)
	defer srv.Close()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := socketCall(t, c, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"fake client"}}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := socketCall(t, c, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`); err != nil {
		t.Fatal(err)
	}
	callID := <-src.calls
	_ = c.Close()
	deadline := time.Now().Add(3 * time.Second)
	for len(src.seen()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := src.seen()
	want := []string{"connected", "named:fake client", "gone"}
	if len(got) != len(want) {
		t.Fatalf("events = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	if callID == 0 || callID != src.ids["connected"] || callID != src.ids["gone"] {
		t.Errorf("ids: call %d, events %v", callID, src.ids)
	}
}
