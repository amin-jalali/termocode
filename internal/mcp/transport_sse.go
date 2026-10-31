package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/extproc"
	"github.com/amin-jalali/termocode/internal/netmsg"
)

// The old HTTP+SSE transport (MCP 2024-11-05, CF-11), only as a fallback:
// when a Streamable HTTP POST of initialize gets a 4xx, the spec's
// backwards-compatibility rule says to GET the URL as an event stream.
// Its first event "endpoint" names where to POST messages; every answer
// comes back on the stream. The endpoint must be on the server's origin.

// legacyEndpoint resolves the endpoint event against the stream's URL
// and refuses another origin.
func legacyEndpoint(base, ep string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u, err := b.Parse(strings.TrimSpace(ep))
	if err != nil {
		return "", fmt.Errorf("bad endpoint %q", ep)
	}
	if u.Scheme != b.Scheme || u.Host != b.Host {
		return "", fmt.Errorf("the server's endpoint is on another host (%s)", u.Host)
	}
	return u.String(), nil
}

// sseConn is one old-transport connection (an rpcConn).
type sseConn struct {
	url      string
	headers  map[string]string
	o        HTTPOptions
	hc       *http.Client
	handle   func(m extproc.Message, reply replyFunc)
	endpoint string

	mu      sync.Mutex
	pending map[string]chan extproc.Message
	ids     atomic.Int64

	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// dialLegacy opens the stream and waits for the endpoint event.
func dialLegacy(ctx context.Context, rawURL string, headers map[string]string, o HTTPOptions, handle func(extproc.Message, replyFunc)) (*sseConn, error) {
	hc := o.Client
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	life, cancel := context.WithCancel(context.Background())
	c := &sseConn{url: rawURL, headers: headers, o: o, hc: hc, handle: handle, pending: map[string]chan extproc.Message{},
		ctx: life, cancel: cancel, done: make(chan struct{})}
	req, err := c.request(life, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	type opened struct {
		resp *http.Response
		err  error
	}
	got := make(chan opened, 1)
	crash.Go("an MCP stream", func() {
		resp, err := hc.Do(req)
		got <- opened{resp, err}
	})
	var op opened
	select {
	case op = <-got:
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	if op.err != nil {
		cancel()
		if why, ok := netmsg.Why(op.err); ok {
			return nil, &ConnError{Host: hostOf(rawURL), Why: why, Err: op.err}
		}
		return nil, fmt.Errorf("MCP %s: %w", hostOf(rawURL), unwrapURLError(op.err))
	}
	resp := op.resp
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		ae := parseAuthenticate(resp.Header.Get("WWW-Authenticate"))
		drain(resp)
		cancel()
		return nil, ae
	case resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"):
		err := statusError(resp)
		cancel()
		return nil, fmt.Errorf("not an MCP server (no event stream): %w", err)
	}
	endpoint := make(chan string, 1)
	crash.Go("an MCP stream", func() { c.read(resp, endpoint) })
	select {
	case ep, ok := <-endpoint:
		if !ok {
			c.Close()
			return nil, errors.New("the server's event stream ended before it named its endpoint")
		}
		u, err := legacyEndpoint(rawURL, ep)
		if err != nil {
			c.Close()
			return nil, err
		}
		c.endpoint = u
		return c, nil
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
}

func (c *sseConn) request(ctx context.Context, method, u string, body []byte) (*http.Request, error) {
	var rd *bytes.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, u, rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, u, nil)
	}
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Authorization") == "" && c.o.Token != nil {
		if tok := c.o.Token(ctx); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	return req, nil
}

// read runs the stream: the endpoint event first, then the messages.
func (c *sseConn) read(resp *http.Response, endpoint chan<- string) {
	defer c.end()
	defer resp.Body.Close()
	sent := false
	defer func() {
		if !sent {
			close(endpoint)
		}
	}()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), extproc.MaxMessage)
	var data strings.Builder
	event := ""
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			d := data.String()
			data.Reset()
			ev := event
			event = ""
			switch {
			case ev == "endpoint" && !sent:
				endpoint <- d
				sent = true
			case d != "" && (ev == "" || ev == "message"):
				var m extproc.Message
				if json.Unmarshal([]byte(d), &m) == nil {
					c.take(m)
				}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
}

// take hands an answer to its caller, or a server message to the client.
func (c *sseConn) take(m extproc.Message) {
	if m.Method == "" {
		key := string(bytes.TrimSpace(m.ID))
		c.mu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
		return
	}
	if c.handle == nil {
		return
	}
	c.handle(m, func(result any, rerr *extproc.Error) {
		if !m.IsRequest() {
			return
		}
		out := map[string]any{"jsonrpc": "2.0", "id": m.ID}
		if rerr != nil {
			out["error"] = rerr
		} else {
			out["result"] = result
		}
		crash.Go("an MCP request", func() { _ = c.post(c.ctx, out) })
	})
}

// post sends one message to the endpoint (the answer comes on the stream).
func (c *sseConn) post(ctx context.Context, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodPost, c.endpoint, body)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if why, ok := netmsg.Why(err); ok {
			return &ConnError{Host: hostOf(c.url), Why: why, Err: err}
		}
		return fmt.Errorf("MCP %s: %w", hostOf(c.url), unwrapURLError(err))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		ae := parseAuthenticate(resp.Header.Get("WWW-Authenticate"))
		drain(resp)
		return ae
	}
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	drain(resp)
	return nil
}

func (c *sseConn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed() {
		return nil, extproc.ErrClosed
	}
	id := c.ids.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan extproc.Message, 1)
	c.mu.Lock()
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err := c.post(pctx, msg)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		crash.Go("an MCP request", func() {
			_ = c.Notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled by the user"})
		})
		return nil, ctx.Err()
	case <-c.done:
		return nil, extproc.ErrClosed
	}
}

func (c *sseConn) Notify(method string, params any) error {
	if c.closed() {
		return extproc.ErrClosed
	}
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	return c.post(ctx, msg)
}

func (c *sseConn) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *sseConn) end() {
	c.closeOnce.Do(func() {
		c.cancel()
		if t, ok := c.hc.Transport.(interface{ CloseIdleConnections() }); ok {
			t.CloseIdleConnections()
		}
		close(c.done)
	})
}

func (c *sseConn) Close()                { c.end() }
func (c *sseConn) Done() <-chan struct{} { return c.done }

// legacyFallback: a 4xx (not 401/403: those are *AuthError) to the
// first POST, per the spec's backwards-compatibility rule.
func legacyFallback(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code >= 400 && se.Code < 500 && se.Code != http.StatusUnauthorized && se.Code != http.StatusForbidden
}

// startLegacy connects with the old transport and runs the handshake.
func startLegacy(ctx context.Context, s Server, ho HTTPOptions, opts Options) (*Process, error) {
	c := &Client{opts: opts}
	ictx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	sc, err := dialLegacy(ictx, s.URL, s.Headers, ho, c.handle)
	if err != nil {
		return nil, err
	}
	c.conn = sc
	p := &Process{Client: c, done: make(chan struct{}), closeFn: sc.Close, legacy: true}
	crash.Go("an MCP server", func() {
		<-sc.Done()
		close(p.done)
	})
	if err := c.Initialize(ictx); err != nil {
		p.Close()
		return nil, err
	}
	opts.log("connected to %s with the old transport (HTTP+SSE)", hostOf(s.URL))
	return p, nil
}
