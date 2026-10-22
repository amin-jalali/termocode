// Package extproc runs Tier 2 extensions: separate processes, in any
// language, that talk JSON-RPC 2.0 to termocode over stdin/stdout.
//
// Framing is the LSP one ("Content-Length: N\r\n\r\n" + N bytes of UTF-8
// JSON), so every language that has an LSP library already has a codec.
// The message shapes follow mobocode's agent/rpc.go (request, response,
// notification, error object). The MCP client (internal/mcp) uses the
// same Conn with newline framing: one JSON object per line
// (NewConnWith, FramingNewline).
//
// This file is the codec and Conn (request/response matching, timeouts,
// a non-blocking write queue). host.go starts, supervises and stops the
// processes. The app glue is internal/app/ext_proc.go; the design is
// docs/adr/0009-process-extensions.md and the user docs are
// docs/extensions.md "Process extensions (Tier 2)".
package extproc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxMessage caps one message body. A bigger Content-Length is a
// protocol error (the stream cannot be trusted any more).
const MaxMessage = 8 << 20

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
	CodeRequestFailed  = -32000 // the handler ran and failed
)

var (
	// ErrTimeout: no response before the context ended.
	ErrTimeout = errors.New("extension did not answer in time")
	// ErrClosed: the connection (the process) is gone.
	ErrClosed = errors.New("extension connection closed")
	// ErrQueueFull: the process does not read its stdin.
	ErrQueueFull = errors.New("extension is not reading its input")
)

// Error is a JSON-RPC error object; it is also the error a Call returns
// when the peer answered with one.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	// Data is optional machine-readable detail (JSON), e.g. {"reason":"declined"}.
	Data json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// Message is any JSON-RPC 2.0 message as read from the wire.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func hasID(id json.RawMessage) bool {
	t := bytes.TrimSpace(id)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

// IsRequest: a method and an id (an answer is expected).
func (m Message) IsRequest() bool { return m.Method != "" && hasID(m.ID) }

// IsNotification: a method and no id.
func (m Message) IsNotification() bool { return m.Method != "" && !hasID(m.ID) }

// IsResponse: an id and no method.
func (m Message) IsResponse() bool { return m.Method == "" && hasID(m.ID) }

// WriteMessage writes v as one framed message.
func WriteMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, b)
}

func writeFrame(w io.Writer, body []byte) error {
	var buf bytes.Buffer
	buf.Grow(len(body) + 32)
	buf.WriteString("Content-Length: ")
	buf.WriteString(strconv.Itoa(len(body)))
	buf.WriteString("\r\n\r\n")
	buf.Write(body)
	_, err := w.Write(buf.Bytes())
	return err
}

// ReadMessage reads one framed message body. io.EOF means the stream
// ended cleanly between messages.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	first := true
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if first && line == "" && errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("reading header: %w", err)
		}
		first = false
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("bad header line %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad Content-Length %q", value)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, errors.New("message without Content-Length")
	}
	if length > MaxMessage {
		return nil, fmt.Errorf("message too big (%d bytes, max %d)", length, MaxMessage)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	return body, nil
}

// Framing is how messages are cut on the wire.
type Framing int

const (
	// FramingContentLength is the LSP framing (extensions, the default).
	FramingContentLength Framing = iota
	// FramingNewline is one JSON message per line (MCP stdio).
	FramingNewline
)

// WriteLineMessage writes v as one line of JSON. encoding/json escapes
// newlines inside strings, so the body never holds a raw '\n'.
func WriteLineMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeLine(w, b)
}

func writeLine(w io.Writer, body []byte) error {
	buf := make([]byte, 0, len(body)+1)
	buf = append(buf, body...)
	buf = append(buf, '\n')
	_, err := w.Write(buf)
	return err
}

// ReadLineMessage reads the next JSON line. Empty lines are dropped;
// lines that do not start like JSON ('{' or '[') are dropped too and
// passed to skipped (when not nil): servers print banners on stdout. A
// line longer than MaxMessage is an error. io.EOF: clean end.
func ReadLineMessage(r *bufio.Reader, skipped func(line string)) ([]byte, error) {
	for {
		var line []byte
		for {
			part, err := r.ReadSlice('\n')
			if len(line)+len(part) > MaxMessage+2 {
				return nil, fmt.Errorf("message too big (over %d bytes)", MaxMessage)
			}
			line = append(line, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				if errors.Is(err, io.EOF) && len(bytes.TrimSpace(line)) > 0 {
					break // last line without '\n'
				}
				if errors.Is(err, io.EOF) {
					return nil, io.EOF
				}
				return nil, fmt.Errorf("reading line: %w", err)
			}
			break
		}
		t := bytes.TrimSpace(line)
		if len(t) == 0 {
			continue
		}
		if t[0] != '{' && t[0] != '[' {
			if skipped != nil {
				skipped(string(t))
			}
			continue
		}
		return t, nil
	}
}

// response builds a success or error response. JSON-RPC wants "result"
// present (null is fine) on success, so it is not omitempty here.
func response(id json.RawMessage, result any, rerr *Error) any {
	if !hasID(id) {
		id = json.RawMessage("null")
	}
	if rerr != nil {
		return struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   *Error          `json:"error"`
		}{"2.0", id, rerr}
	}
	return struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", id, result}
}

// Handler gets the peer's requests and notifications, in order, on the
// Conn's reader goroutine. It must not block for long: answers to this
// side's own Calls are read on the same goroutine. Answer a request with
// Conn.Reply (from any goroutine).
type Handler func(c *Conn, m Message)

// queueSize is how many outgoing messages may wait for the process to
// read them before writes fail with ErrQueueFull.
const queueSize = 512

// Conn is one JSON-RPC connection. Writes never block the caller: they go
// through a queue that one goroutine drains.
type Conn struct {
	r       io.Reader
	w       io.Writer
	handler Handler
	opts    ConnOptions

	out    chan []byte
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[string]chan Message
	closed  bool
	err     error

	done      chan struct{} // closed when the reader ends
	closeOnce sync.Once
	quit      chan struct{} // closed by Close
}

// ConnOptions changes how a Conn talks. The zero value is the extension
// protocol (Content-Length framing).
type ConnOptions struct {
	Framing Framing
	// Skipped (optional) gets the non-JSON lines a newline peer printed.
	Skipped func(line string)
	// OnCancel (optional) is called with the id of a Call whose ctx ended
	// before the answer (MCP tells the server with notifications/cancelled).
	OnCancel func(c *Conn, id json.RawMessage)
}

// NewConn starts the reader and writer goroutines.
func NewConn(r io.Reader, w io.Writer, h Handler) *Conn {
	return NewConnWith(r, w, h, ConnOptions{})
}

// NewConnWith is NewConn with options (newline framing for MCP).
func NewConnWith(r io.Reader, w io.Writer, h Handler, opts ConnOptions) *Conn {
	c := &Conn{
		r: r, w: w, handler: h, opts: opts,
		out:     make(chan []byte, queueSize),
		pending: map[string]chan Message{},
		done:    make(chan struct{}),
		quit:    make(chan struct{}),
	}
	go c.readLoop()
	go c.writeLoop()
	return c
}

// Done is closed when the connection stops reading (EOF, error, Close).
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the reader stopped (nil before that, or after a clean EOF).
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close stops both goroutines and fails every pending Call.
func (c *Conn) Close() {
	c.closeOnce.Do(func() {
		close(c.quit)
		if cl, ok := c.w.(io.Closer); ok {
			_ = cl.Close()
		}
		if cl, ok := c.r.(io.Closer); ok {
			_ = cl.Close()
		}
		c.failPending()
	})
}

func (c *Conn) failPending() {
	c.mu.Lock()
	c.closed = true
	p := c.pending
	c.pending = map[string]chan Message{}
	c.mu.Unlock()
	for _, ch := range p {
		close(ch)
	}
}

func idKey(id json.RawMessage) string { return string(bytes.TrimSpace(id)) }

func (c *Conn) readLoop() {
	defer close(c.done)
	defer c.failPending()
	br := bufio.NewReaderSize(c.r, 64<<10)
	for {
		var body []byte
		var err error
		if c.opts.Framing == FramingNewline {
			body, err = ReadLineMessage(br, c.opts.Skipped)
		} else {
			body, err = ReadMessage(br)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
			}
			return
		}
		var m Message
		if err := json.Unmarshal(body, &m); err != nil {
			_ = c.send(response(nil, nil, &Error{Code: CodeParseError, Message: err.Error()}))
			continue
		}
		switch {
		case m.IsResponse():
			c.mu.Lock()
			ch, ok := c.pending[idKey(m.ID)]
			delete(c.pending, idKey(m.ID))
			c.mu.Unlock()
			if ok {
				ch <- m // buffered (1)
			}
		case m.Method != "":
			if c.handler != nil {
				c.handler(c, m)
			} else if m.IsRequest() {
				_ = c.Reply(m.ID, nil, &Error{Code: CodeMethodNotFound, Message: "method not found: " + m.Method})
			}
		}
	}
}

func (c *Conn) writeLoop() {
	for {
		select {
		case b := <-c.out:
			write := writeFrame
			if c.opts.Framing == FramingNewline {
				write = writeLine
			}
			if err := write(c.w, b); err != nil {
				c.Close()
				return
			}
		case <-c.quit:
			return
		}
	}
}

func (c *Conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	select {
	case c.out <- b:
		return nil
	case <-c.quit:
		return ErrClosed
	default:
		return ErrQueueFull
	}
}

// Call sends a request and waits for its response, ctx's end or the
// connection's end. A JSON-RPC error answer is returned as *Error.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan Message, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.pending[key] = ch
	c.mu.Unlock()
	req := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", id, method, params}
	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		if c.opts.OnCancel != nil {
			c.opts.OnCancel(c, json.RawMessage(key))
		}
		return nil, fmt.Errorf("%s: %w", method, ErrTimeout)
	}
}

// Notify sends a notification (no answer).
func (c *Conn) Notify(method string, params any) error {
	return c.send(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", method, params})
}

// Reply answers the peer's request id with result, or with rerr when it
// is not nil.
func (c *Conn) Reply(id json.RawMessage, result any, rerr *Error) error {
	return c.send(response(id, result, rerr))
}
