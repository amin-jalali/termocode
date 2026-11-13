package dbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTP engines (DBX-68..71): servers that are not SQL databases and that
// the app reaches over their own HTTP(S) API with net/http only (no
// driver module). Each engine registers itself in httpEngines from its
// own file: its title, default ports, how to open a session and how the
// console guard classifies one request.

// HTTPEngine describes one engine.
type HTTPEngine struct {
	ID      string
	Title   string
	Port    int // plain HTTP (or TCP) port
	TLSPort int // 0: same as Port
	// Hint is the add picker's hint ("N1QL over the query service").
	Hint string
	// Database is the default database name ("" = none).
	Database string
	// Open makes a session of c (it does not ping).
	Open func(c Conn, password string) (Session, error)
	// Classify judges one console request (nil: the SQL classifier).
	Classify func(string) Kind
	// Schemes are the URL schemes ParseURL maps to the engine; a scheme
	// that ends in "s" (or "+s") turns TLS on.
	Schemes []string
	// LineBased engines run one command per console line (no ';').
	LineBased bool
	// FixURL adjusts a connection parsed from one of Schemes (nil: none).
	FixURL func(*Conn)
	// QueryParams keeps the URL's query as Conn.Params ("?warehouse=WH").
	QueryParams bool
	// URLExample makes the add flow ask for a URL of this shape instead
	// of a host and port (the cloud warehouses, DBX-66).
	URLExample string
}

var httpEngines = map[string]HTTPEngine{}

func registerHTTPEngine(e HTTPEngine) { httpEngines[e.ID] = e }

// IsHTTPEngine reports a driver served by an HTTP engine.
func IsHTTPEngine(driver string) bool {
	_, ok := httpEngines[normDriver(driver)]
	return ok
}

// HTTPEngineByID is the engine of a driver.
func HTTPEngineByID(driver string) (HTTPEngine, bool) {
	e, ok := httpEngines[normDriver(driver)]
	return e, ok
}

// HTTPEngines lists the engines by title (the add picker's order).
func HTTPEngines() []HTTPEngine {
	out := make([]HTTPEngine, 0, len(httpEngines))
	for _, e := range httpEngines {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// OpenHTTPEngine makes a session of c for its engine; the caller pings.
func OpenHTTPEngine(c Conn, password string) (Session, error) {
	e, ok := HTTPEngineByID(c.Driver)
	if !ok {
		return nil, fmt.Errorf("unsupported database driver")
	}
	return e.Open(c, password)
}

// engineClassify is the engine's classifier of driver, or nil.
func engineClassify(driver string) func(string) Kind {
	if e, ok := httpEngines[normDriver(driver)]; ok {
		return e.Classify
	}
	return nil
}

// engineForScheme is the engine of a URL scheme and whether it means TLS.
func engineForScheme(scheme string) (HTTPEngine, bool, bool) {
	for _, e := range httpEngines {
		for _, s := range e.Schemes {
			if s == scheme {
				tls := scheme != e.ID && (strings.HasSuffix(scheme, "s") || strings.HasSuffix(scheme, "+s") || scheme == "https")
				return e, tls, true
			}
		}
	}
	return HTTPEngine{}, false, false
}

// httpEngineDefaultPort is the default port of an engine (TLS or not).
func httpEngineDefaultPort(driver string, tls bool) int {
	e, ok := httpEngines[normDriver(driver)]
	if !ok {
		return 0
	}
	if tls && e.TLSPort != 0 {
		return e.TLSPort
	}
	return e.Port
}

// errNoTx is the answer of Begin, Commit and Rollback on an engine
// without transactions.
func errNoTx(title string) error {
	return fmt.Errorf("%s has no transactions here; each request commits on its own", title)
}

// httpEngine is the shared HTTP part of an engine session: the base URL,
// the client (through the SSH tunnel's dialer when there is one), basic
// auth and Cancel. The password goes in the Authorization header only;
// errors never show it.
type httpEngine struct {
	title    string
	base     string
	user     string
	password string
	client   *http.Client
	header   http.Header

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

// newHTTPEngine builds the base URL of c (its http(s) URL, else the
// host, port and TLS flag) and the client.
func newHTTPEngine(title string, c Conn, password string) (*httpEngine, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.MaxIdleConns, tr.IdleConnTimeout = 2, 90*time.Second
	if c.Dial != nil {
		dial := c.Dial
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) { return dial(ctx, network, addr) }
	}
	tls := c.TLS || (c.SSL != nil && c.SSL.Mode != "" && c.SSL.Mode != SSLDisable)
	var base string
	user := c.User
	if u, err := url.Parse(c.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		if u.User != nil && user == "" {
			user = u.User.Username()
		}
		tls = tls || u.Scheme == "https"
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		if tls {
			u.Scheme = "https"
		}
		base = strings.TrimRight(u.String(), "/")
	} else {
		if c.Host == "" {
			return nil, fmt.Errorf("%s: no server address", title)
		}
		port := c.Port
		if port == 0 {
			port = httpEngineDefaultPort(c.Driver, tls)
		}
		scheme := "http"
		if tls {
			scheme = "https"
		}
		base = scheme + "://" + net.JoinHostPort(c.Host, strconv.Itoa(port))
	}
	if tls {
		cfg, err := TLSConfig(c.SSL, c.Host)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			tr.TLSClientConfig = cfg
		}
	}
	return &httpEngine{title: title, base: base, user: user, password: password, client: &http.Client{Transport: tr}, header: http.Header{}}, nil
}

// run registers a cancel for one request.
func (e *httpEngine) run(parent context.Context) (context.Context, func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, nil, errors.New(e.title + ": session closed")
	}
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	return ctx, func() {
		cancel()
		e.mu.Lock()
		e.cancel = nil
		e.mu.Unlock()
	}, nil
}

// httpError is a non-2xx answer: the status and the server's message.
type httpError struct {
	Status int
	Msg    string
}

func (h *httpError) Error() string {
	if h.Msg == "" {
		return fmt.Sprintf("HTTP %d", h.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", h.Status, h.Msg)
}

// httpEngineMaxBody is the most bytes read from one answer.
const httpEngineMaxBody = 32 << 20

// do sends one request and returns the answer body. body may be nil;
// ctype "" means JSON. A non-2xx answer is an *httpError with the
// server's message (the password scrubbed out).
func (e *httpEngine) do(parent context.Context, method, path string, body []byte, ctype string) ([]byte, error) {
	ctx, done, err := e.run(parent)
	if err != nil {
		return nil, err
	}
	defer done()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.base+path, rd)
	if err != nil {
		return nil, e.clean(err)
	}
	for k, v := range e.header {
		req.Header[k] = v
	}
	if body != nil {
		if ctype == "" {
			ctype = "application/json"
		}
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("Accept", "application/json")
	if e.user != "" || e.password != "" {
		if req.Header.Get("Authorization") == "" {
			req.SetBasicAuth(e.user, e.password)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, e.clean(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, httpEngineMaxBody))
	if err != nil {
		return nil, e.clean(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return data, &httpError{Status: resp.StatusCode, Msg: e.scrub(httpErrorMessage(data))}
	}
	return data, nil
}

// doJSON sends v as JSON (nil: no body) and decodes the answer into out
// (nil: ignored).
func (e *httpEngine) doJSON(ctx context.Context, method, path string, v, out any) error {
	var body []byte
	if v != nil {
		var err error
		if body, err = json.Marshal(v); err != nil {
			return err
		}
	}
	data, err := e.do(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: unexpected answer: %v", e.title, err)
	}
	return nil
}

// httpErrorMessage digs the message out of a JSON error body (the common
// shapes), else the first line of the text.
func httpErrorMessage(data []byte) string {
	var v map[string]any
	if json.Unmarshal(data, &v) == nil {
		for _, k := range []string{"error", "message", "errors", "reason", "msg"} {
			if m := jsonMessage(v[k]); m != "" {
				return m
			}
		}
	}
	s := strings.TrimSpace(string(data))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

func jsonMessage(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		for _, k := range []string{"reason", "message", "msg"} {
			if s, ok := x[k].(string); ok && s != "" {
				return s
			}
		}
		if t, ok := x["type"].(string); ok {
			return t
		}
	case []any:
		if len(x) > 0 {
			return jsonMessage(x[0])
		}
	}
	return ""
}

func (e *httpEngine) scrub(s string) string {
	if e.password != "" {
		s = strings.ReplaceAll(s, e.password, "***")
	}
	return s
}

func (e *httpEngine) clean(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errors.New(e.scrub(err.Error()))
}

func (e *httpEngine) Cancel() {
	e.mu.Lock()
	c := e.cancel
	e.mu.Unlock()
	if c != nil {
		c()
	}
}

func (e *httpEngine) Close() error {
	e.mu.Lock()
	e.closed = true
	c := e.cancel
	e.mu.Unlock()
	if c != nil {
		c()
	}
	e.client.CloseIdleConnections()
	return nil
}

// clone is a second session on the same server (a read-only twin).
func (e *httpEngine) clone() *httpEngine {
	h := http.Header{}
	for k, v := range e.header {
		h[k] = append([]string(nil), v...)
	}
	return &httpEngine{title: e.title, base: e.base, user: e.user, password: e.password, client: e.client, header: h}
}

// ── JSON documents as a grid ─────────────────────────────────────────────

// DocsResult turns JSON documents into a grid: the union of the top-level
// keys as columns (first seen first), nested values as compact JSON. A
// document that is not an object is one "value" column. MaxRows (> 0)
// stops early and marks the result truncated.
func DocsResult(docs []json.RawMessage, maxRows int) Result {
	return docsResult(docs, maxRows, false)
}

// TypedDocsResult is DocsResult with numbers and booleans as typed cells
// (Kind "number", "bool"), so a document view can write them back as JSON
// numbers and booleans, apart from strings that look like them.
func TypedDocsResult(docs []json.RawMessage, maxRows int) Result {
	return docsResult(docs, maxRows, true)
}

func docsResult(docs []json.RawMessage, maxRows int, typed bool) Result {
	var res Result
	index := map[string]int{}
	var rows []map[string]json.RawMessage
	for _, d := range docs {
		if maxRows > 0 && len(rows) >= maxRows {
			res.Truncated = true
			break
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(d, &obj) != nil || obj == nil {
			rows = append(rows, map[string]json.RawMessage{"value": d})
			if _, ok := index["value"]; !ok {
				index["value"] = len(res.Columns)
				res.Columns = append(res.Columns, Column{Name: "value"})
			}
			continue
		}
		keys := orderedKeys(d)
		for _, k := range keys {
			if _, ok := index[k]; !ok {
				index[k] = len(res.Columns)
				res.Columns = append(res.Columns, Column{Name: k})
			}
		}
		rows = append(rows, obj)
	}
	for _, r := range rows {
		row := make([]Cell, len(res.Columns))
		for i, c := range res.Columns {
			v, ok := r[c.Name]
			if !ok {
				row[i] = Cell{Null: true}
				continue
			}
			row[i] = JSONCell(v)
			if typed {
				row[i] = typedJSONCell(v)
			}
		}
		res.Rows = append(res.Rows, row)
	}
	return res
}

// JSONCell is one JSON value as a cell: strings bare, null as NULL,
// numbers and booleans as written, objects and arrays as compact JSON.
func JSONCell(v json.RawMessage) Cell {
	t := bytes.TrimSpace(v)
	if len(t) == 0 || string(t) == "null" {
		return Cell{Null: true}
	}
	switch t[0] {
	case '"':
		var s string
		if json.Unmarshal(t, &s) == nil {
			return Cell{Text: s}
		}
	case '{', '[':
		var b bytes.Buffer
		if json.Compact(&b, t) == nil {
			return Cell{Text: b.String(), Kind: "json"}
		}
	}
	return Cell{Text: string(t)}
}

// typedJSONCell is JSONCell with numbers and booleans typed.
func typedJSONCell(v json.RawMessage) Cell {
	c := JSONCell(v)
	if c.Null || c.Kind != "" {
		return c
	}
	switch t := strings.TrimSpace(string(v)); {
	case t == "true" || t == "false":
		c.Kind = "bool"
	case len(t) > 0 && t[0] != '"' && json.Valid([]byte(t)):
		c.Kind = "number"
	}
	return c
}

// orderedKeys lists the top-level keys of a JSON object in the order
// they are written (a Go map loses it).
func orderedKeys(d json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(d))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

// stripLineComments drops whole lines that start with "--", "//" or "#"
// (the console's "-- @conn" header) and trims the rest.
func stripLineComments(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "--") || strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// authClient is a client that adds the session's headers and basic auth
// to every request, for helpers that take a plain *http.Client.
func (e *httpEngine) authClient() *http.Client {
	return &http.Client{Transport: authTransport{e: e, next: e.client.Transport}}
}

type authTransport struct {
	e    *httpEngine
	next http.RoundTripper
}

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range a.e.header {
		r.Header[k] = v
	}
	if (a.e.user != "" || a.e.password != "") && r.Header.Get("Authorization") == "" {
		r.SetBasicAuth(a.e.user, a.e.password)
	}
	return a.next.RoundTrip(r)
}
