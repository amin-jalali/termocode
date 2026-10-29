package extproc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValuesThatAreNotJSONAreErrors(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, func() {}); err == nil || buf.Len() != 0 {
		t.Fatalf("WriteMessage: err %v, wrote %q", err, buf.String())
	}
	if err := WriteLineMessage(&buf, make(chan int)); err == nil || buf.Len() != 0 {
		t.Fatalf("WriteLineMessage: err %v, wrote %q", err, buf.String())
	}
	a, _ := pipePair(t, nil, nil)
	if err := a.Notify("x", func() {}); err == nil {
		t.Fatal("Notify with a func param")
	}
	if _, err := a.Call(context.Background(), "x", func() {}); err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("Call with a func param: %v", err)
	}
	// The failed call left nothing pending: a later call still works.
	a.mu.Lock()
	n := len(a.pending)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d calls left pending", n)
	}
}

func TestQueueFullWhenPeerNeverReads(t *testing.T) {
	r, w := io.Pipe() // nobody reads w's other end
	defer r.Close()
	in, inW := io.Pipe() // and nothing ever comes in
	defer inW.Close()
	c := NewConn(in, w, nil)
	defer c.Close()
	var err error
	for i := 0; i < queueSize+10 && err == nil; i++ {
		err = c.Notify("spam", nil)
	}
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
	// A call that cannot be queued fails at once.
	if _, err := c.Call(context.Background(), "x", nil); !errors.Is(err, ErrQueueFull) && !errors.Is(err, ErrClosed) {
		t.Fatalf("call err = %v", err)
	}
}

func TestCancelledCallTellsOnCancel(t *testing.T) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	got := make(chan json.RawMessage, 1)
	a := NewConnWith(ar, aw, nil, ConnOptions{OnCancel: func(_ *Conn, id json.RawMessage) { got <- id }})
	b := NewConn(br, bw, func(*Conn, Message) {}) // never answers
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.Call(ctx, "slow", nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	select {
	case id := <-got:
		if string(id) != "1" {
			t.Fatalf("id %s", id)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("OnCancel not called")
	}
}

func TestPeerWithoutHandlerAndBadJSON(t *testing.T) {
	// b has no handler: requests get "method not found".
	a, _ := pipePair(t, nil, nil)
	_, err := a.Call(context.Background(), "anything", nil)
	var rerr *Error
	if !errors.As(err, &rerr) || rerr.Code != CodeMethodNotFound {
		t.Fatalf("err = %v", err)
	}

	// A frame that is not JSON gets a parse error with a null id.
	in := strings.NewReader("Content-Length: 5\r\n\r\n{nope")
	var out syncBuffer
	c := NewConn(in, &out, nil)
	<-c.Done()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), `"id":null`) {
		if time.Now().After(deadline) {
			t.Fatalf("no parse error answer: %q", out.String())
		}
		time.Sleep(time.Millisecond)
	}
	c.Close()
	body, err := ReadMessage(bufio.NewReader(strings.NewReader(out.String())))
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if err := json.Unmarshal(body, &m); err != nil || m.Error == nil || m.Error.Code != CodeParseError {
		t.Fatalf("answer %s", body)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestInWorkspaceEdges(t *testing.T) {
	if _, err := InWorkspace("", "x"); !errors.Is(err, ErrOutside) {
		t.Fatalf("empty root: %v", err)
	}
	if _, err := InWorkspace(filepath.Join(t.TempDir(), "gone"), "x"); err == nil {
		t.Fatal("missing root must fail")
	}
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// A path through a file (not a folder) cannot be resolved.
	if _, err := InWorkspace(root, "file.txt/below"); err == nil {
		t.Fatal("path below a file must fail")
	}
	// Not existing yet, but inside: fine.
	if got, err := InWorkspace(root, "new/dir/x.go"); err != nil || got != filepath.Join(root, "new", "dir", "x.go") {
		t.Fatalf("got %q %v", got, err)
	}
}
