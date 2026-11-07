package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WSDir is the direction of one WebSocket message.
type WSDir int

const (
	WSOut WSDir = iota // sent by us
	WSIn               // received
)

// WSMessage is one message in a WebSocket session log.
type WSMessage struct {
	Dir    WSDir     `json:"dir"`
	Text   string    `json:"text"`
	Binary bool      `json:"binary,omitempty"`
	At     time.Time `json:"at"`
}

// wsLogMax is how many messages a session keeps (newest win).
const wsLogMax = 1000

// wsLog is a capped, concurrency-safe message list.
type wsLog struct {
	mu   sync.Mutex
	max  int
	msgs []WSMessage
}

func (l *wsLog) add(m WSMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, m)
	if over := len(l.msgs) - l.max; over > 0 {
		l.msgs = append(l.msgs[:0:0], l.msgs[over:]...)
	}
}

func (l *wsLog) all() []WSMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]WSMessage(nil), l.msgs...)
}

// WSConn is an open WebSocket session.
type WSConn struct {
	conn  *websocket.Conn
	log   *wsLog
	close func()
	now   func() time.Time
}

// IsWebSocket reports whether r is a WebSocket request: the method is
// WEBSOCKET or the URL starts with ws:// or wss://.
func IsWebSocket(r Request) bool {
	if strings.EqualFold(r.Method, "WEBSOCKET") {
		return true
	}
	u := strings.ToLower(strings.TrimSpace(r.URL))
	return strings.HasPrefix(u, "ws://") || strings.HasPrefix(u, "wss://")
}

// WSMessages splits a WebSocket request body into its saved messages
// (separated by `===` lines; blank ones are dropped).
func WSMessages(body *string) []string {
	if body == nil {
		return nil
	}
	var out []string
	var cur []string
	flush := func() {
		if m := strings.TrimSpace(strings.Join(cur, "\n")); m != "" {
			out = append(out, m)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(*body, "\n") {
		if strings.TrimSpace(line) == "===" {
			flush()
			continue
		}
		cur = append(cur, strings.TrimRight(line, "\r"))
	}
	flush()
	return out
}

// DialWS opens a WebSocket session for rr (ws, wss, http or https URL)
// with its headers. It uses the same transport rules as Send: proxy,
// Insecure and the AllowIP guard.
func (c *Client) DialWS(ctx context.Context, rr Resolved, h Hooks) (*WSConn, error) {
	u := strings.TrimSpace(rr.URL)
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "ws://") && !strings.HasPrefix(low, "wss://") &&
		!strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return nil, fmt.Errorf("websocket: the URL must start with ws:// or wss://")
	}
	proxy, override, perr := requestProxy(rr.Directives, h)
	if perr != nil {
		return nil, perr
	}
	base, _, err := c.transport(h, proxy, override)
	if err != nil {
		return nil, fmt.Errorf("websocket: TLS: %w", err)
	}
	// WebSocket needs HTTP/1.1: a clone without HTTP/2.
	t := base.Clone()
	t.ForceAttemptHTTP2 = false
	t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	if t.TLSClientConfig != nil {
		t.TLSClientConfig.NextProtos = []string{"http/1.1"}
	}
	hdr := http.Header{}
	for _, hd := range rr.Headers {
		hdr.Add(hd.Name, hd.Value)
	}
	if c.opts.UserAgent != "" && hdr.Get("User-Agent") == "" {
		hdr.Set("User-Agent", c.opts.UserAgent)
	}
	dctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: t},
		HTTPHeader: hdr,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.CloseIdleConnections()
		var be *blockedError
		if errors.As(err, &be) {
			return nil, be
		}
		return nil, fmt.Errorf("websocket: %w", err)
	}
	conn.SetReadLimit(c.opts.MaxBody)
	return &WSConn{conn: conn, log: &wsLog{max: wsLogMax}, close: t.CloseIdleConnections, now: time.Now}, nil
}

// Send writes one text message.
func (w *WSConn) Send(ctx context.Context, text string) error {
	if err := w.conn.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		return err
	}
	w.log.add(WSMessage{Dir: WSOut, Text: text, At: w.now()})
	return nil
}

// Read waits for the next message.
func (w *WSConn) Read(ctx context.Context) (WSMessage, error) {
	typ, b, err := w.conn.Read(ctx)
	if err != nil {
		return WSMessage{}, err
	}
	m := WSMessage{Dir: WSIn, Text: string(b), Binary: typ == websocket.MessageBinary, At: w.now()}
	w.log.add(m)
	return m, nil
}

// Log returns the session's messages, oldest first.
func (w *WSConn) Log() []WSMessage { return w.log.all() }

// Close ends the session with a normal close.
func (w *WSConn) Close() error {
	err := w.conn.Close(websocket.StatusNormalClosure, "")
	w.close()
	return err
}

// WSExchange opens a session, sends msgs in order, then collects what
// arrives until wait passes with no new message or maxMsgs came in,
// and closes. It is the AI tool's one-shot WebSocket call (API-17).
func (c *Client) WSExchange(ctx context.Context, rr Resolved, h Hooks, msgs []string, wait time.Duration, maxMsgs int) ([]WSMessage, error) {
	conn, err := c.DialWS(ctx, rr, h)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	for _, m := range msgs {
		if err := conn.Send(ctx, m); err != nil {
			return conn.Log(), fmt.Errorf("websocket send: %w", err)
		}
	}
	in := 0
	for in < maxMsgs {
		rctx, cancel := context.WithTimeout(ctx, wait)
		_, err := conn.Read(rctx)
		cancel()
		if err != nil {
			break // the wait passed, or the server closed
		}
		in++
	}
	return conn.Log(), nil
}
