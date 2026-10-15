package extproc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFramingRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	msgs := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"x": "é ✓"}},
		map[string]any{"jsonrpc": "2.0", "method": "log", "params": map[string]any{"message": "a\r\nb"}},
	}
	for _, m := range msgs {
		if err := WriteMessage(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.HasPrefix(buf.String(), "Content-Length: ") {
		t.Fatalf("no Content-Length header: %q", buf.String())
	}
	r := bufio.NewReader(&buf)
	for i := range msgs {
		b, err := ReadMessage(r)
		if err != nil {
			t.Fatalf("message %d: %v", i, err)
		}
		var m Message
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("message %d: %v (%q)", i, err, b)
		}
		if i == 0 && (m.Method != "initialize" || string(m.ID) != "1") {
			t.Errorf("first = %+v", m)
		}
		if i == 1 && (m.Method != "log" || m.ID != nil) {
			t.Errorf("second = %+v", m)
		}
	}
	if _, err := ReadMessage(r); !errors.Is(err, io.EOF) {
		t.Errorf("after the last message: %v, want EOF", err)
	}
}

func TestReadMessageHeaders(t *testing.T) {
	// Header names are case-insensitive; other headers are ignored.
	in := "content-length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}"
	b, err := ReadMessage(bufio.NewReader(strings.NewReader(in)))
	if err != nil || string(b) != "{}" {
		t.Fatalf("got %q, %v", b, err)
	}
	for name, in := range map[string]string{
		"no length":  "Content-Type: x\r\n\r\n{}",
		"bad length": "Content-Length: abc\r\n\r\n{}",
		"too big":    "Content-Length: 999999999\r\n\r\n{}",
		"short body": "Content-Length: 10\r\n\r\n{}",
	} {
		if _, err := ReadMessage(bufio.NewReader(strings.NewReader(in))); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// pipePair connects two Conns back to back.
func pipePair(t *testing.T, ha, hb Handler) (a, b *Conn) {
	t.Helper()
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	a = NewConn(ar, aw, ha)
	b = NewConn(br, bw, hb)
	t.Cleanup(func() {
		a.Close()
		b.Close()
		_ = ar.Close()
		_ = br.Close()
	})
	return a, b
}

func TestCallResponseMatching(t *testing.T) {
	// b answers "slow" only after "fast" was answered: responses arrive
	// out of order and must still reach the right caller.
	release := make(chan struct{})
	client, _ := pipePair(t, nil, func(c *Conn, m Message) {
		var p struct{ N int }
		_ = json.Unmarshal(m.Params, &p)
		go func() {
			if m.Method == "slow" {
				<-release
			}
			_ = c.Reply(m.ID, map[string]int{"n": p.N * 10}, nil)
		}()
	})

	type res struct {
		raw json.RawMessage
		err error
	}
	slow := make(chan res, 1)
	go func() {
		r, err := client.Call(context.Background(), "slow", map[string]int{"n": 1})
		slow <- res{r, err}
	}()
	r, err := client.Call(context.Background(), "fast", map[string]int{"n": 2})
	if err != nil || string(r) != `{"n":20}` {
		t.Fatalf("fast = %s, %v", r, err)
	}
	close(release)
	s := <-slow
	if s.err != nil || string(s.raw) != `{"n":10}` {
		t.Fatalf("slow = %s, %v", s.raw, s.err)
	}
}

func TestCallTimeoutAndClose(t *testing.T) {
	got := make(chan Message, 4)
	client, _ := pipePair(t, nil, func(c *Conn, m Message) { got <- m }) // never answers
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.Call(ctx, "never", nil)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if m := <-got; m.Method != "never" || !m.IsRequest() {
		t.Errorf("server got %+v", m)
	}
	// A call pending when the connection closes ends with ErrClosed.
	done := make(chan error, 1)
	go func() {
		_, err := client.Call(context.Background(), "pending", nil)
		done <- err
	}()
	<-got // the request is out
	client.Close()
	if err := <-done; !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if _, err := client.Call(context.Background(), "after", nil); !errors.Is(err, ErrClosed) {
		t.Errorf("call after close = %v, want ErrClosed", err)
	}
}

func TestErrorResponseAndNotify(t *testing.T) {
	notes := make(chan Message, 1)
	client, _ := pipePair(t, func(c *Conn, m Message) { notes <- m }, func(c *Conn, m Message) {
		if m.IsRequest() {
			_ = c.Reply(m.ID, nil, &Error{Code: CodeMethodNotFound, Message: "no " + m.Method})
			return
		}
		_ = c.Notify("echo", json.RawMessage(m.Params))
	})
	_, err := client.Call(context.Background(), "nope", nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeMethodNotFound || re.Message != "no nope" {
		t.Fatalf("err = %v", err)
	}
	if err := client.Notify("ping", map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	m := <-notes
	if m.Method != "echo" || !m.IsNotification() || string(m.Params) != `{"a":"b"}` {
		t.Fatalf("notification = %+v", m)
	}
}

// MC-0: newline-delimited JSON (MCP stdio). Empty lines and lines that
// are not JSON (a server printing a banner on stdout) are skipped and
// reported; \r\n works; a line over MaxMessage is an error.
func TestReadLineMessage(t *testing.T) {
	var skipped []string
	in := "{\"a\":1}\n\nnpm WARN banner\r\n{\"b\":2}\r\n  [1,2]\n{\"c\":3}"
	r := bufio.NewReader(strings.NewReader(in))
	var got []string
	for {
		b, err := ReadLineMessage(r, func(l string) { skipped = append(skipped, l) })
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(b))
	}
	want := []string{`{"a":1}`, `{"b":2}`, `[1,2]`, `{"c":3}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(skipped) != 1 || skipped[0] != "npm WARN banner" {
		t.Errorf("skipped %q", skipped)
	}

	big := "{\"x\":\"" + strings.Repeat("a", MaxMessage) + "\"}\n"
	if _, err := ReadLineMessage(bufio.NewReader(strings.NewReader(big)), nil); err == nil || errors.Is(err, io.EOF) {
		t.Errorf("line over MaxMessage: err = %v, want a size error", err)
	}
}

func TestWriteLineMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteLineMessage(&buf, map[string]any{"jsonrpc": "2.0", "method": "x", "params": map[string]any{"s": "a\nb"}}); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if strings.Count(s, "\n") != 1 || !strings.HasSuffix(s, "}\n") || strings.Contains(s, "Content-Length") {
		t.Fatalf("frame %q", s)
	}
}

// Two newline Conns back to back: calls, notifications and replies use
// one JSON object per line. A Call whose ctx ends reports its id to
// OnCancel (MCP sends notifications/cancelled with it).
func TestNewlineConn(t *testing.T) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	var mu sync.Mutex
	var cancelled []string
	opts := ConnOptions{Framing: FramingNewline, OnCancel: func(c *Conn, id json.RawMessage) {
		mu.Lock()
		cancelled = append(cancelled, string(id))
		mu.Unlock()
	}}
	a := NewConnWith(ar, aw, nil, opts)
	b := NewConnWith(br, bw, func(c *Conn, m Message) {
		if m.Method == "never" {
			return
		}
		_ = c.Reply(m.ID, map[string]string{"m": m.Method}, nil)
	}, ConnOptions{Framing: FramingNewline})
	t.Cleanup(func() { a.Close(); b.Close(); _ = ar.Close(); _ = br.Close() })
	r, err := a.Call(context.Background(), "hello", nil)
	if err != nil || string(r) != `{"m":"hello"}` {
		t.Fatalf("call = %s, %v", r, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := a.Call(ctx, "never", nil); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cancelled) != 1 || cancelled[0] != "2" {
		t.Errorf("OnCancel ids %q, want [2]", cancelled)
	}
}

// A reply with a nil result must still carry "result": null.
func TestReplyNilResult(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMessage(&buf, response(json.RawMessage("7"), nil, nil)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"result":null`) || !strings.Contains(buf.String(), `"id":7`) {
		t.Fatalf("reply = %q", buf.String())
	}
}
