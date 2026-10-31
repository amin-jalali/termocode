package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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

// Streamable HTTP transport (MC-5, MCP 2025-03-26 … 2025-11-25):
//
//   - every client message is a POST of one JSON-RPC object; the answer
//     is either application/json or a text/event-stream that may carry
//     server notifications / requests before the answer;
//   - Mcp-Session-Id from the initialize answer goes on every later
//     request; MCP-Protocol-Version once the version is negotiated;
//   - a stream that ends before the answer is resumed with GET and
//     Last-Event-ID (up to maxResumes times);
//   - 401 is an *AuthError (the WWW-Authenticate fields), 404 with a
//     session is ErrSessionExpired;
//   - Close ends the session with DELETE.
//
// Secrets: header values and tokens never go to the log; errors carry
// only the status and a short, redacted-by-the-caller body.

// AuthError is a 401 answer: the server wants (another) token.
type AuthError struct {
	// ResourceMetadata is the protected-resource metadata URL from
	// WWW-Authenticate ("" when the server did not send one).
	ResourceMetadata string
	Scope            string // scope the server asks for ("" none)
	Detail           string // error / error_description
	// InsufficientScope: a 403 error="insufficient_scope": the token is
	// good but needs Scope too (step-up sign-in).
	InsufficientScope bool
}

func (e *AuthError) Error() string {
	s := "not authorized (HTTP 401)"
	if e.InsufficientScope {
		s = "not allowed with this sign-in (HTTP 403)"
	}
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	return s
}

// ErrSessionExpired: the server forgot the session (HTTP 404). The
// connection ends; the next call connects again.
var ErrSessionExpired = errors.New("the MCP server ended the session")

// HTTPOptions tune a remote connection.
type HTTPOptions struct {
	// Token returns the OAuth access token to send as a bearer ("" none).
	// A static Authorization header in the config wins over it.
	Token func(ctx context.Context) string
	// Refresh is called once after a 401 to a request that carried a
	// Token (ae: the 401's fields): it gets a new one and reports
	// whether to try again.
	Refresh func(ctx context.Context, ae *AuthError) bool
	// Client is the HTTP client (tests); nil makes one.
	Client *http.Client
}

// maxResumes caps the GET retries of one broken stream.
const maxResumes = 3

// httpConn is one Streamable HTTP connection (an rpcConn).
type httpConn struct {
	url     string
	headers map[string]string
	o       HTTPOptions
	hc      *http.Client
	handle  func(m extproc.Message, reply replyFunc)

	mu      sync.Mutex
	session string
	version string

	ids       atomic.Int64
	ctx       context.Context // ends with Close
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

func newHTTPConn(rawURL string, headers map[string]string, o HTTPOptions, handle func(extproc.Message, replyFunc)) *httpConn {
	hc := o.Client
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &httpConn{url: rawURL, headers: headers, o: o, hc: hc, handle: handle, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (c *httpConn) Done() <-chan struct{} { return c.done }

func (c *httpConn) setProtocolVersion(v string) {
	c.mu.Lock()
	c.version = v
	c.mu.Unlock()
}

func (c *httpConn) closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// end closes the connection; del sends DELETE for the session first.
func (c *httpConn) end(del bool) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		sid := c.session
		c.mu.Unlock()
		if del && sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if resp, err := c.send(ctx, http.MethodDelete, nil, "", nil); err == nil {
				drain(resp)
			}
			cancel()
		}
		c.cancel()
		if t, ok := c.hc.Transport.(interface{ CloseIdleConnections() }); ok {
			t.CloseIdleConnections()
		}
		close(c.done)
	})
}

// Close ends the session (DELETE) and the connection.
func (c *httpConn) Close() { c.end(true) }

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
}

// send builds and sends one request, without the 401 handling.
func (c *httpConn) send(ctx context.Context, method string, body []byte, accept string, extra map[string]string) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url, rd)
	if err != nil {
		return nil, err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	c.mu.Lock()
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	c.mu.Unlock()
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Authorization") == "" && c.o.Token != nil {
		if tok := c.o.Token(ctx); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
			req = req.WithContext(context.WithValue(ctx, sentTokenKey{}, true))
		}
	}
	return c.hc.Do(req)
}

type sentTokenKey struct{}

// do is send with the 401 rule (one refresh, one retry) and the session
// expiry rule.
func (c *httpConn) do(ctx context.Context, method string, body []byte, accept string, extra map[string]string) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.send(ctx, method, body, accept, extra)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if why, ok := netmsg.Why(err); ok {
				return nil, &ConnError{Host: hostOf(c.url), Why: why, Err: err}
			}
			return nil, fmt.Errorf("MCP %s: %w", hostOf(c.url), unwrapURLError(err))
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			ae := parseAuthenticate(resp.Header.Get("WWW-Authenticate"))
			drain(resp)
			sent, _ := resp.Request.Context().Value(sentTokenKey{}).(bool)
			if sent && attempt == 0 && c.o.Refresh != nil && c.o.Refresh(ctx, ae) {
				continue
			}
			return nil, ae
		case http.StatusForbidden:
			h := resp.Header.Get("WWW-Authenticate")
			if authParams(h)["error"] == "insufficient_scope" {
				drain(resp)
				ae := parseAuthenticate(h)
				ae.InsufficientScope = true
				return nil, ae
			}
		case http.StatusNotFound:
			c.mu.Lock()
			had := c.session != ""
			c.mu.Unlock()
			if had {
				drain(resp)
				c.end(false)
				return nil, ErrSessionExpired
			}
		}
		return resp, nil
	}
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "server"
}

// unwrapURLError drops the *url.Error wrapper (it repeats the URL).
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// StatusError is a non-2xx answer: its code and a short body.
type StatusError struct {
	Code int
	Body string // trimmed, at most 200 bytes ("" none)
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("HTTP %d", e.Code)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body)
}

// statusError is a non-2xx answer with a short body.
func statusError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	resp.Body.Close()
	msg := strings.TrimSpace(string(b))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return &StatusError{Code: resp.StatusCode, Body: msg}
}

// parseAuthenticate reads the Bearer challenge of WWW-Authenticate.
func parseAuthenticate(h string) *AuthError {
	ae := &AuthError{}
	params := authParams(h)
	ae.ResourceMetadata = params["resource_metadata"]
	ae.Scope = params["scope"]
	ae.Detail = params["error_description"]
	if ae.Detail == "" {
		ae.Detail = params["error"]
	}
	return ae
}

// authParams parses `Bearer k="v", k2=v2` (quoted strings may hold
// commas and escaped quotes).
func authParams(h string) map[string]string {
	out := map[string]string{}
	s := strings.TrimSpace(h)
	if i := strings.IndexByte(s, ' '); i > 0 && !strings.Contains(s[:i], "=") {
		s = s[i+1:]
	}
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ,")
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(s[:eq]))
		s = s[eq+1:]
		var val string
		if strings.HasPrefix(s, `"`) {
			var b strings.Builder
			i := 1
			for ; i < len(s); i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					b.WriteByte(s[i])
					continue
				}
				if s[i] == '"' {
					break
				}
				b.WriteByte(s[i])
			}
			val = b.String()
			if i < len(s) {
				s = s[i+1:]
			} else {
				s = ""
			}
		} else {
			end := strings.IndexByte(s, ',')
			if end < 0 {
				end = len(s)
			}
			val = strings.TrimSpace(s[:end])
			s = s[end:]
		}
		out[key] = val
	}
	return out
}

// Call sends one request and waits for its answer (JSON or SSE).
func (c *httpConn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed() {
		return nil, extproc.ErrClosed
	}
	id := c.ids.Add(1)
	idRaw := json.RawMessage(strconv.FormatInt(id, 10))
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel) // Close ends every request
	defer stop()

	res, err := c.call(rctx, method, body, idRaw)
	if err != nil && ctx.Err() != nil {
		c.notifyCancelled(idRaw)
		return nil, ctx.Err()
	}
	// end cancels c.ctx before it closes done: a call cut by Close can
	// get here in between, so the cancelled c.ctx counts as closed too.
	if err != nil && (c.ctx.Err() != nil || c.closed()) && !errors.Is(err, ErrSessionExpired) {
		return nil, extproc.ErrClosed
	}
	return res, err
}

func (c *httpConn) call(ctx context.Context, method string, body []byte, id json.RawMessage) (json.RawMessage, error) {
	resp, err := c.do(ctx, http.MethodPost, body, "application/json, text/event-stream", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, statusError(resp)
	}
	if method == "initialize" {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.mu.Lock()
			c.session = sid
			c.mu.Unlock()
		}
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "text/event-stream" {
		return c.readStream(ctx, resp.Body, id)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, extproc.MaxMessage+1))
	if err != nil {
		return nil, err
	}
	if len(b) > extproc.MaxMessage {
		return nil, errors.New("the MCP server's answer is too big")
	}
	var msgs []extproc.Message
	if t := bytes.TrimSpace(b); len(t) > 0 && t[0] == '[' {
		if err := json.Unmarshal(t, &msgs); err != nil {
			return nil, fmt.Errorf("bad answer: %w", err)
		}
	} else {
		var m extproc.Message
		if err := json.Unmarshal(t, &m); err != nil {
			return nil, fmt.Errorf("bad answer: %w", err)
		}
		msgs = []extproc.Message{m}
	}
	for _, m := range msgs {
		if res, ok, err := c.take(m, id); ok {
			return res, err
		}
	}
	return nil, errors.New("the MCP server sent no answer")
}

// take handles one incoming message: the answer to id (ok=true), or a
// server request / notification (dispatched).
func (c *httpConn) take(m extproc.Message, id json.RawMessage) (json.RawMessage, bool, error) {
	if m.Method == "" {
		if bytes.Equal(bytes.TrimSpace(m.ID), id) {
			if m.Error != nil {
				return nil, true, m.Error
			}
			return m.Result, true, nil
		}
		return nil, false, nil
	}
	c.dispatch(m)
	return nil, false, nil
}

// dispatch hands a server message to the client; answers to requests go
// back as their own POST.
func (c *httpConn) dispatch(m extproc.Message) {
	if c.handle == nil {
		return
	}
	reply := func(result any, rerr *extproc.Error) {
		if !m.IsRequest() {
			return
		}
		out := map[string]any{"jsonrpc": "2.0", "id": m.ID}
		if rerr != nil {
			out["error"] = rerr
		} else {
			out["result"] = result
		}
		crash.Go("an MCP request", func() { c.post(out) })
	}
	c.handle(m, reply)
}

// post sends a message that expects no answer (202).
func (c *httpConn) post(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	resp, err := c.do(ctx, http.MethodPost, body, "application/json, text/event-stream", nil)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return statusError(resp)
	}
	drain(resp)
	return nil
}

// Notify sends a notification.
func (c *httpConn) Notify(method string, params any) error {
	if c.closed() {
		return extproc.ErrClosed
	}
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	return c.post(msg)
}

func (c *httpConn) notifyCancelled(id json.RawMessage) {
	crash.Go("an MCP request", func() {
		_ = c.Notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled by the user"})
	})
}

// readStream reads SSE events until the answer of id. A stream that ends
// early is resumed with GET + Last-Event-ID.
func (c *httpConn) readStream(ctx context.Context, body io.ReadCloser, id json.RawMessage) (json.RawMessage, error) {
	lastID := ""
	for resumes := 0; ; resumes++ {
		res, found, last, err := c.scan(body, id)
		body.Close()
		if last != "" {
			lastID = last
		}
		if found {
			return res, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if lastID == "" || resumes >= maxResumes {
			if err == nil {
				err = errors.New("the MCP server ended the stream without an answer")
			}
			return nil, err
		}
		resp, err := c.do(ctx, http.MethodGet, nil, "text/event-stream", map[string]string{"Last-Event-ID": lastID})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("the stream broke and could not be resumed: %w", statusError(resp))
		}
		body = resp.Body
	}
}

// scan reads one SSE stream. found reports the answer of id; last is the
// last event id seen.
func (c *httpConn) scan(r io.Reader, id json.RawMessage) (res json.RawMessage, found bool, last string, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), extproc.MaxMessage)
	var data strings.Builder
	evID := ""
	flush := func() (json.RawMessage, bool, error) {
		defer func() { data.Reset(); evID = "" }()
		if evID != "" {
			last = evID
		}
		if data.Len() == 0 {
			return nil, false, nil
		}
		var m extproc.Message
		if json.Unmarshal([]byte(data.String()), &m) != nil {
			return nil, false, nil // not JSON-RPC: skip it
		}
		return c.take(m, id)
	}
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		if line == "" {
			if r, ok, e := flush(); ok {
				return r, true, last, e
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / keep-alive
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		case "id":
			evID = value
		}
	}
	if r, ok, e := flush(); ok {
		return r, true, last, e
	}
	return nil, false, last, sc.Err()
}

// ── Connection ─────────────────────────────────────────────────────────

// StartHTTP connects to a remote server (s already expanded) and runs
// the handshake. A 401 comes back as *AuthError.
func StartHTTP(ctx context.Context, s Server, ho HTTPOptions, opts Options) (*Process, error) {
	if err := checkURL(s.URL); err != nil {
		return nil, err
	}
	c := &Client{opts: opts}
	hc := newHTTPConn(s.URL, s.Headers, ho, c.handle)
	c.conn = hc
	p := &Process{Client: c, done: make(chan struct{}), closeFn: hc.Close}
	crash.Go("an MCP server", func() {
		<-hc.Done()
		close(p.done)
	})
	ictx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	if err := c.Initialize(ictx); err != nil {
		p.Close()
		if legacyFallback(err) {
			opts.log("the server answered the POST with %v: trying the old transport (HTTP+SSE)", err)
			return startLegacy(ctx, s, ho, opts)
		}
		return nil, err
	}
	opts.log("connected to %s", hostOf(s.URL))
	return p, nil
}

// checkURL allows https, and http only to this machine (tokens must not
// cross the network in clear text).
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid url %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if IsLoopback(u.Hostname()) {
			return nil
		}
		return errors.New("insecure url: use https:// (http only for localhost)")
	}
	return fmt.Errorf("invalid url scheme %q", u.Scheme)
}

// IsLoopback reports localhost / 127.x / ::1.
func IsLoopback(host string) bool {
	return host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.")
}

// ConnError is a remote server that could not be reached: Why says the
// cause in plain words (refused, name not found, no answer, proxy).
type ConnError struct {
	Host, Why string
	Err       error
}

func (e *ConnError) Error() string { return "no connection to " + e.Host + ": " + e.Why }
func (e *ConnError) Unwrap() error { return e.Err }
