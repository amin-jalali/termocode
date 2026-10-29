package extproc

import (
	"bufio"
	"bytes"
	"io"
	"testing"
	"time"
)

// FuzzReadMessage reads any bytes as Content-Length framed messages. Each
// body fits the limit and frames back to a body that reads the same.
func FuzzReadMessage(f *testing.F) {
	for _, s := range []string{
		"Content-Length: 2\r\n\r\n{}",
		"content-length:  7 \r\nContent-Type: x\r\n\r\n{\"a\":1}Content-Length: 0\r\n\r\n",
		"Content-Length: -1\r\n\r\n",
		"Content-Length: 99999999999\r\n\r\n",
		"no colon\r\n\r\n",
		"\r\n",
		"Content-Length: 5\r\n\r\n{}",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		r := bufio.NewReader(bytes.NewReader(b))
		for {
			body, err := ReadMessage(r)
			if err != nil {
				return
			}
			if len(body) > MaxMessage {
				t.Fatalf("body of %d bytes passed the limit", len(body))
			}
			var w bytes.Buffer
			if err := writeFrame(&w, body); err != nil {
				t.Fatal(err)
			}
			back, err := ReadMessage(bufio.NewReader(&w))
			if err != nil || !bytes.Equal(back, body) {
				t.Fatalf("frame round trip: %q -> %q (%v)", body, back, err)
			}
		}
	})
}

// FuzzReadLineMessage reads any bytes as newline framed JSON. Each
// message is one trimmed line that starts like JSON and reads back the
// same after writeLine.
func FuzzReadLineMessage(f *testing.F) {
	for _, s := range []string{
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n",
		"banner text\n\n  [1,2]  \r\n{\"last\":true}",
		"{\"a\":1}\r\n",
		"",
		"\n\n\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		r := bufio.NewReaderSize(bytes.NewReader(b), 16)
		for {
			body, err := ReadLineMessage(r, func(string) {})
			if err != nil {
				return
			}
			if len(body) == 0 || (body[0] != '{' && body[0] != '[') || bytes.IndexByte(body, '\n') >= 0 {
				t.Fatalf("bad message %q", body)
			}
			if !bytes.Equal(body, bytes.TrimSpace(body)) {
				t.Fatalf("message not trimmed: %q", body)
			}
			var w bytes.Buffer
			if err := writeLine(&w, body); err != nil {
				t.Fatal(err)
			}
			back, err := ReadLineMessage(bufio.NewReader(&w), nil)
			if err != nil || !bytes.Equal(back, body) {
				t.Fatalf("line round trip: %q -> %q (%v)", body, back, err)
			}
		}
	})
}

// FuzzConn runs a whole connection over any input in both framings: it
// must stop at the end of the input without a panic or a hang.
func FuzzConn(f *testing.F) {
	for _, s := range []string{
		"Content-Length: 46\r\n\r\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"x\"}       ",
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n{\"id\":7,\"result\":null}\nnot json\n",
		"{\"id\":[1],\"method\":\"\"}\n",
		"{broken\n",
	} {
		f.Add([]byte(s), false)
		f.Add([]byte(s), true)
	}
	f.Fuzz(func(t *testing.T, b []byte, lines bool) {
		opts := ConnOptions{}
		if lines {
			opts.Framing = FramingNewline
		}
		c := NewConnWith(bytes.NewReader(b), io.Discard, nil, opts)
		defer c.Close()
		select {
		case <-c.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("connection did not stop at the end of input")
		}
	})
}
