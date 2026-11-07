package socketio

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Options tune a connection.
type Options struct {
	Auth     json.RawMessage // the CONNECT payload (an object), nil = none
	Header   http.Header     // handshake headers
	Insecure bool            // skip the TLS certificate check
}

// Event is one event the server sent.
type Event struct {
	Name string
	Args []json.RawMessage
	// Binary is set for an event with binary attachments; they are not
	// decoded, their places hold {"_placeholder":true,"num":n}.
	Binary bool
}

// eventsMax is how many unread events are kept; more are counted as
// dropped so the reader never stalls the pings.
const eventsMax = 1024

// readLimit caps one WebSocket frame.
const readLimit = 16 << 20

// Client is one Socket.IO connection to one namespace.
type Client struct {
	ws  *websocket.Conn
	ns  string
	sid string

	pingWait time.Duration // pingInterval + pingTimeout

	events chan Event
	done   chan struct{}
	stop   context.CancelFunc
	tr     *http.Transport

	mu      sync.Mutex
	acks    map[int]chan []json.RawMessage
	nextID  int
	err     error
	dropped int
	closed  bool
}

type openPacket struct {
	SID          string `json:"sid"`
	PingInterval int    `json:"pingInterval"`
	PingTimeout  int    `json:"pingTimeout"`
}

// Dial connects to t and joins its namespace. ctx bounds the handshake.
func Dial(ctx context.Context, t Target, o Options) (*Client, error) {
	if t.engine == "" {
		return nil, errors.New("socket.io: no target")
	}
	if len(o.Auth) > 0 {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(o.Auth, &obj); err != nil {
			return nil, errors.New("socket.io: @auth must be a JSON object")
		}
	}
	tr := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{ServerName: t.Host, InsecureSkipVerify: o.Insecure, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}, //nolint:gosec // insecure only on the user's choice
	}
	ws, resp, err := websocket.Dial(ctx, t.engine, &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}, HTTPHeader: o.Header})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		tr.CloseIdleConnections()
		return nil, fmt.Errorf("socket.io: websocket: %w", err)
	}
	ws.SetReadLimit(readLimit)
	c := &Client{ws: ws, ns: t.Namespace, tr: tr, events: make(chan Event, eventsMax), done: make(chan struct{}), acks: map[int]chan []json.RawMessage{}}
	if err := c.handshake(ctx, o.Auth); err != nil {
		_ = ws.Close(websocket.StatusNormalClosure, "")
		tr.CloseIdleConnections()
		return nil, err
	}
	rctx, stop := context.WithCancel(context.Background())
	c.stop = stop
	go c.read(rctx)
	return c, nil
}

// handshake reads the Engine.IO open packet, sends CONNECT and waits for
// the namespace's CONNECT or CONNECT_ERROR.
func (c *Client) handshake(ctx context.Context, auth json.RawMessage) error {
	msg, err := c.readText(ctx)
	if err != nil {
		return fmt.Errorf("socket.io: no open packet: %w", err)
	}
	if !strings.HasPrefix(msg, "0") {
		return fmt.Errorf("socket.io: the server did not open an Engine.IO v4 session (got %q)", clip(msg))
	}
	var op openPacket
	if err := json.Unmarshal([]byte(msg[1:]), &op); err != nil {
		return fmt.Errorf("socket.io: bad open packet: %v", err)
	}
	c.pingWait = time.Duration(op.PingInterval+op.PingTimeout) * time.Millisecond
	if c.pingWait <= 0 {
		c.pingWait = 45 * time.Second
	}
	if err := c.send(ctx, packet{typ: pConnect, ns: c.ns, id: -1, data: auth}); err != nil {
		return err
	}
	for {
		msg, err := c.readText(ctx)
		if err != nil {
			return fmt.Errorf("socket.io: no CONNECT answer: %w", err)
		}
		switch {
		case strings.HasPrefix(msg, "2"):
			if err := c.ws.Write(ctx, websocket.MessageText, []byte("3"+msg[1:])); err != nil {
				return err
			}
			continue
		case !strings.HasPrefix(msg, "4"):
			continue
		}
		p, err := decodePacket(msg[1:])
		if err != nil {
			return err
		}
		if p.ns != c.ns {
			continue
		}
		switch p.typ {
		case pConnect:
			var ok struct {
				SID string `json:"sid"`
			}
			_ = json.Unmarshal(p.data, &ok)
			c.sid = ok.SID
			return nil
		case pConnectError:
			var e struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(p.data, &e) != nil || e.Message == "" {
				e.Message = string(p.data)
			}
			return fmt.Errorf("socket.io: the server refused namespace %s: %s", c.ns, e.Message)
		}
	}
}

// readText reads the next text frame (binary frames are skipped).
func (c *Client) readText(ctx context.Context) (string, error) {
	for {
		typ, b, err := c.ws.Read(ctx)
		if err != nil {
			return "", err
		}
		if typ == websocket.MessageText {
			return string(b), nil
		}
	}
}

func (c *Client) send(ctx context.Context, p packet) error {
	return c.ws.Write(ctx, websocket.MessageText, []byte("4"+p.encode()))
}

// SID is the session id the server gave the namespace.
func (c *Client) SID() string { return c.sid }

// Events delivers the server's events; it closes when the connection ends.
func (c *Client) Events() <-chan Event { return c.events }

// Err is why the connection ended (nil while open or after Close).
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Dropped is how many events were dropped because nobody read them.
func (c *Client) Dropped() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dropped
}

// Emit sends the event name with args (each one JSON value).
func (c *Client) Emit(ctx context.Context, name string, args []json.RawMessage) error {
	data, err := eventData(name, args)
	if err != nil {
		return err
	}
	return c.send(ctx, packet{typ: pEvent, ns: c.ns, id: -1, data: data})
}

// EmitAck sends the event and waits for the server's ack.
func (c *Client) EmitAck(ctx context.Context, name string, args []json.RawMessage) ([]json.RawMessage, error) {
	data, err := eventData(name, args)
	if err != nil {
		return nil, err
	}
	ch := make(chan []json.RawMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("socket.io: the connection is closed")
	}
	id := c.nextID
	c.nextID++
	c.acks[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.acks, id)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, packet{typ: pEvent, ns: c.ns, id: id, data: data}); err != nil {
		return nil, err
	}
	select {
	case a := <-ch:
		return a, nil
	case <-c.done:
		if err := c.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("socket.io: the connection closed before the ack")
	case <-ctx.Done():
		return nil, fmt.Errorf("socket.io: no ack for %q: %w", name, ctx.Err())
	}
}

// Close leaves the namespace and closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	already := c.closed
	c.closed = true
	c.mu.Unlock()
	if !already {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.send(ctx, packet{typ: pDisconnect, ns: c.ns, id: -1})
		cancel()
	}
	// The server may have closed first; the session is over either way.
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
	if c.stop != nil {
		c.stop()
	}
	<-c.done
	c.tr.CloseIdleConnections()
	return nil
}

// fail ends the reader with err unless the client was closed.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if !c.closed && c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

func (c *Client) read(ctx context.Context) {
	defer close(c.done)
	defer close(c.events)
	for {
		rctx, cancel := context.WithTimeout(ctx, c.pingWait)
		typ, b, err := c.ws.Read(rctx)
		timedOut := rctx.Err() == context.DeadlineExceeded && ctx.Err() == nil
		cancel()
		if err != nil {
			if timedOut {
				c.fail(errors.New("socket.io: the server stopped sending pings"))
			} else {
				c.fail(fmt.Errorf("socket.io: connection lost: %w", err))
			}
			return
		}
		if typ != websocket.MessageText || len(b) == 0 {
			continue // binary attachments are not decoded
		}
		msg := string(b)
		switch msg[0] {
		case '2': // ping
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.ws.Write(wctx, websocket.MessageText, []byte("3"+msg[1:]))
			wcancel()
			if err != nil {
				c.fail(fmt.Errorf("socket.io: pong: %w", err))
				return
			}
		case '1': // close
			c.fail(errors.New("socket.io: the server closed the session"))
			return
		case '4':
			if stop := c.onPacket(ctx, msg[1:]); stop {
				return
			}
		}
	}
}

// onPacket handles one Socket.IO packet; true ends the reader.
func (c *Client) onPacket(ctx context.Context, s string) bool {
	p, err := decodePacket(s)
	if err != nil || p.ns != c.ns {
		return false
	}
	switch p.typ {
	case pEvent, pBinaryEvent:
		name, args, err := splitEvent(p.data)
		if err != nil {
			return false
		}
		if p.id >= 0 {
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = c.send(wctx, packet{typ: pAck, ns: c.ns, id: p.id, data: json.RawMessage("[]")})
			cancel()
		}
		select {
		case c.events <- Event{Name: name, Args: args, Binary: p.typ == pBinaryEvent}:
		default:
			c.mu.Lock()
			c.dropped++
			c.mu.Unlock()
		}
	case pAck, pBinaryAck:
		var args []json.RawMessage
		_ = json.Unmarshal(p.data, &args)
		c.mu.Lock()
		ch := c.acks[p.id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- args:
			default:
			}
		}
	case pDisconnect:
		c.fail(errors.New("socket.io: the server disconnected the namespace"))
		return true
	}
	return false
}
