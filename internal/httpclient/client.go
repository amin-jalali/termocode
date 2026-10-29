package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptrace"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Defaults (settings http_timeout, http_max_redirects,
// http_max_body_mb).
const (
	DefaultTimeout      = 30 * time.Second
	DefaultMaxRedirects = 10
	DefaultMaxBody      = 20 << 20
	connectTimeout      = 10 * time.Second
	readChunk           = 64 << 10
	progressEvery       = 256 << 10
)

// Options configure a Client.
type Options struct {
	Timeout      time.Duration
	MaxRedirects int
	MaxBody      int64
	CookieJar    bool
	UserAgent    string
	// Proxy picks the proxy per request; nil = HTTP_PROXY / HTTPS_PROXY
	// / NO_PROXY read when the client is made.
	Proxy func(*http.Request) (*url.URL, error)
	// Resolve looks a host name up for sends with Hooks.AllowIP; nil =
	// net.DefaultResolver. Tests pass a fake.
	Resolve func(ctx context.Context, host string) ([]net.IP, error)
}

// Hooks change one send.
type Hooks struct {
	// Progress is called with the bytes read so far, every 256 KB.
	Progress func(n int64)
	// AllowIP, when set, is asked for every IP the send connects to
	// (also redirect hops and new connections); a refused IP fails the
	// send with "blocked: private address". AI sends set it.
	AllowIP func(net.IP) bool
	// CheckHop, when set, is asked before following a redirect; an
	// error stops the send (AI sends re-gate each hop).
	CheckHop func(url string) error
	// Insecure skips TLS verification for this send only (the user's
	// `k` retry; never for AI sends).
	Insecure bool
}

// Client sends requests. It is safe for concurrent use.
type Client struct {
	opts  Options
	jar   http.CookieJar
	once  sync.Once
	plain *http.Transport
}

// New makes a client.
func New(opts Options) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = DefaultMaxRedirects
	}
	if opts.MaxBody <= 0 {
		opts.MaxBody = DefaultMaxBody
	}
	if opts.Proxy == nil {
		opts.Proxy = envProxy()
	}
	c := &Client{opts: opts}
	if opts.CookieJar {
		c.jar, _ = cookiejar.New(nil)
	}
	return c
}

// Close drops idle connections.
func (c *Client) Close() {
	if c.plain != nil {
		c.plain.CloseIdleConnections()
	}
}

func (c *Client) transport(h Hooks) (*http.Transport, bool) {
	if h.AllowIP == nil && !h.Insecure {
		c.once.Do(func() { c.plain = c.newTransport(h) })
		return c.plain, false
	}
	return c.newTransport(h), true
}

func (c *Client) newTransport(h Hooks) *http.Transport {
	d := &net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}
	t := &http.Transport{
		Proxy:                 c.opts.Proxy,
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   connectTimeout,
		ExpectContinueTimeout: time.Second,
	}
	if h.Insecure {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the user's one-off `k` retry
	}
	if h.AllowIP != nil {
		allow := h.AllowIP
		proxy := c.opts.Proxy
		t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if isProxyAddr(proxy, addr) {
				return d.DialContext(ctx, network, addr)
			}
			ips, err := c.resolve(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				if !allow(ip) {
					lastErr = &blockedError{host: host, ip: ip}
					continue
				}
				conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("lookup %s: no address", host)
			}
			return nil, lastErr
		}
	}
	return t
}

// isProxyAddr reports whether addr is the configured proxy (dialing a
// proxy is not dialing the target).
func isProxyAddr(proxy func(*http.Request) (*url.URL, error), addr string) bool {
	if proxy == nil {
		return false
	}
	for _, scheme := range []string{"http", "https"} {
		u, err := proxy(&http.Request{URL: &url.URL{Scheme: scheme, Host: "termocode-proxy-probe.invalid"}})
		if err != nil || u == nil {
			continue
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443"}[u.Scheme]
			if port == "" {
				port = "80"
			}
		}
		if net.JoinHostPort(u.Hostname(), port) == addr {
			return true
		}
	}
	return false
}

func (c *Client) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if c.opts.Resolve != nil {
		return c.opts.Resolve(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = a.IP
	}
	return ips, nil
}

type blockedError struct {
	host string
	ip   net.IP
}

func (e *blockedError) Error() string {
	return fmt.Sprintf("blocked: private address %s for %s", e.ip, e.host)
}

type hopError struct{ err error }

func (e *hopError) Error() string { return e.err.Error() }

var errTooManyRedirects = errors.New("too many redirects")

// Send runs one request. It never panics and always returns a
// Response; failures set Err and ErrKind.
func (c *Client) Send(ctx context.Context, rr Resolved, h Hooks) *Response {
	r := rr.Request
	resp := &Response{Method: r.Method, URL: r.URL, MaxBody: c.opts.MaxBody}
	start := time.Now()
	timeout := c.opts.Timeout
	if v, ok := r.Directives["timeout"]; ok {
		if d, err := parseTimeout(v); err == nil && d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		resp.fail(ErrKindOther, fmt.Sprintf("not an http(s) URL: %s", RedactURL(r.URL)), start)
		return resp
	}
	var body io.Reader
	bodyText := r.BodyText()
	if r.Body != nil {
		body = strings.NewReader(bodyText)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u.String(), body)
	if err != nil {
		resp.fail(ErrKindOther, err.Error(), start)
		return resp
	}
	for _, hd := range r.Headers {
		if strings.EqualFold(hd.Name, "host") {
			req.Host = hd.Value
			continue
		}
		req.Header.Add(hd.Name, hd.Value)
	}
	if !r.HasHeader("Content-Type") && looksJSON(bodyText) {
		req.Header.Set("Content-Type", "application/json")
	}
	if !r.HasHeader("User-Agent") && c.opts.UserAgent != "" {
		req.Header.Set("User-Agent", c.opts.UserAgent)
	}
	resp.ReqHeaders = headerList(req.Header)
	if r.Body != nil {
		resp.ReqHeaders = append(resp.ReqHeaders, Header{Name: "content-length", Value: fmt.Sprint(len(bodyText))})
		sortHeaders(resp.ReqHeaders)
	}

	var tm timingTrace
	req = req.WithContext(httptrace.WithClientTrace(ctx, tm.trace()))

	tr, oneOff := c.transport(h)
	if oneOff {
		defer tr.CloseIdleConnections()
	}
	_, noRedirect := r.Directives["no-redirect"]
	_, noJar := r.Directives["no-cookie-jar"]
	hc := &http.Client{Transport: tr}
	if c.jar != nil && !noJar {
		hc.Jar = c.jar
	}
	hc.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if noRedirect {
			return http.ErrUseLastResponse
		}
		if len(via) > c.opts.MaxRedirects {
			return errTooManyRedirects
		}
		status := 0
		if next.Response != nil {
			status = next.Response.StatusCode
		}
		resp.Hops = append(resp.Hops, Hop{Status: status, URL: next.URL.String()})
		if h.CheckHop != nil {
			if err := h.CheckHop(next.URL.String()); err != nil {
				return &hopError{err}
			}
		}
		return nil
	}

	hr, err := hc.Do(req)
	if err != nil {
		resp.Timing = tm.result(start)
		resp.failErr(err, start, timeout, c.opts.MaxRedirects)
		return resp
	}
	defer hr.Body.Close()
	resp.URL = hr.Request.URL.String()
	resp.Status = hr.StatusCode
	resp.StatusText = strings.TrimSpace(strings.TrimPrefix(hr.Status, fmt.Sprint(hr.StatusCode)))
	if resp.StatusText == "" {
		resp.StatusText = http.StatusText(hr.StatusCode)
	}
	resp.Proto = hr.Proto
	resp.Headers = headerList(hr.Header)
	resp.ContentType, resp.Charset = parseContentType(hr.Header.Get("Content-Type"))

	var buf bytes.Buffer
	limited := io.LimitReader(hr.Body, c.opts.MaxBody+1)
	chunk := make([]byte, readChunk)
	var next int64 = progressEvery
	for {
		n, rerr := limited.Read(chunk)
		buf.Write(chunk[:n])
		if h.Progress != nil && int64(buf.Len()) >= next {
			h.Progress(int64(buf.Len()))
			for next <= int64(buf.Len()) {
				next += progressEvery
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			resp.Body = buf.Bytes()
			resp.Size = int64(buf.Len())
			resp.Timing = tm.result(start)
			if cerr := ctx.Err(); cerr != nil {
				rerr = cerr
			}
			resp.failErr(rerr, start, timeout, c.opts.MaxRedirects)
			return resp
		}
	}
	b := buf.Bytes()
	if int64(len(b)) > c.opts.MaxBody {
		b = b[:c.opts.MaxBody]
		resp.Truncated = true
	}
	resp.Body = b
	resp.Size = int64(len(b))
	resp.IsJSON = isJSONType(resp.ContentType)
	resp.IsBinary = isBinary(resp.ContentType, b)
	resp.Elapsed = time.Since(start)
	resp.Timing = tm.result(start)
	return resp
}

func (r *Response) fail(kind ErrKind, msg string, start time.Time) {
	r.ErrKind = kind
	r.Err = msg
	r.Elapsed = time.Since(start)
}

func (r *Response) failErr(err error, start time.Time, timeout time.Duration, maxRedirects int) {
	r.Elapsed = time.Since(start)
	var be *blockedError
	var he *hopError
	var ue *url.Error
	var certErr *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var invalid x509.CertificateInvalidError
	var recErr tls.RecordHeaderError
	var opErr *net.OpError
	switch {
	case errors.As(err, &be):
		r.ErrKind, r.Err = ErrKindBlocked, be.Error()
	case errors.As(err, &he):
		r.ErrKind, r.Err = ErrKindBlocked, he.Error()
	case errors.Is(err, errTooManyRedirects):
		r.ErrKind, r.Err = ErrKindRedirects, fmt.Sprintf("stopped after %d redirects", maxRedirects)
		if len(r.Hops) > maxRedirects {
			r.Hops = r.Hops[:maxRedirects]
		}
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		r.ErrKind, r.Err = ErrKindTimeout, "timed out after "+formatTimeout(timeout)
	case errors.Is(err, context.Canceled):
		r.ErrKind, r.Err = ErrKindCancelled, "cancelled after "+formatSeconds(r.Elapsed)
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &invalid), errors.As(err, &recErr),
		strings.Contains(err.Error(), "tls: "), strings.Contains(err.Error(), "x509: "):
		msg := err.Error()
		if errors.As(err, &ue) {
			msg = ue.Err.Error()
		}
		if i := strings.Index(msg, "x509: "); i >= 0 {
			msg = msg[i:]
		} else if i := strings.Index(msg, "tls: "); i >= 0 {
			msg = msg[i+5:]
		}
		r.ErrKind, r.Err = ErrKindTLS, "TLS: "+msg
	case errors.As(err, &opErr):
		r.ErrKind, r.Err = ErrKindDial, opErr.Error()
	default:
		msg := err.Error()
		if errors.As(err, &ue) {
			msg = ue.Err.Error()
		}
		r.ErrKind, r.Err = ErrKindOther, msg
	}
	r.Err = RedactURL(r.Err)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func formatTimeout(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return formatSeconds(d)
}

func formatSeconds(d time.Duration) string {
	s := d.Seconds()
	if s == float64(int64(s)) {
		return fmt.Sprintf("%d s", int64(s))
	}
	return fmt.Sprintf("%.1f s", s)
}

func parseTimeout(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if d, err := time.ParseDuration(v); err == nil {
		return d, nil
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil && fmt.Sprint(n) == v {
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("bad timeout %q", v)
}

func looksJSON(body string) bool {
	t := strings.TrimSpace(body)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// headerList flattens h with lower-cased names, sorted by name.
func headerList(h http.Header) []Header {
	var out []Header
	for k, vs := range h {
		name := strings.ToLower(k)
		for _, v := range vs {
			out = append(out, Header{Name: name, Value: v})
		}
	}
	sortHeaders(out)
	return out
}

func sortHeaders(hs []Header) {
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].Name < hs[j].Name })
}

// timingTrace collects httptrace times.
type timingTrace struct {
	mu                       sync.Mutex
	dnsStart, connStart, tls time.Time
	t                        Timing
	firstByte                time.Time
}

func (tt *timingTrace) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { tt.mu.Lock(); tt.dnsStart = time.Now(); tt.mu.Unlock() },
		DNSDone: func(httptrace.DNSDoneInfo) {
			tt.mu.Lock()
			if !tt.dnsStart.IsZero() {
				tt.t.DNS = time.Since(tt.dnsStart)
			}
			tt.mu.Unlock()
		},
		ConnectStart: func(string, string) { tt.mu.Lock(); tt.connStart = time.Now(); tt.mu.Unlock() },
		ConnectDone: func(string, string, error) {
			tt.mu.Lock()
			if !tt.connStart.IsZero() {
				tt.t.Connect = time.Since(tt.connStart)
			}
			tt.mu.Unlock()
		},
		TLSHandshakeStart: func() { tt.mu.Lock(); tt.tls = time.Now(); tt.mu.Unlock() },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			tt.mu.Lock()
			if !tt.tls.IsZero() {
				tt.t.TLS = time.Since(tt.tls)
			}
			tt.mu.Unlock()
		},
		GotFirstResponseByte: func() { tt.mu.Lock(); tt.firstByte = time.Now(); tt.mu.Unlock() },
	}
}

func (tt *timingTrace) result(start time.Time) Timing {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	t := tt.t
	if !tt.firstByte.IsZero() {
		t.FirstByte = tt.firstByte.Sub(start)
	}
	t.Total = time.Since(start)
	return t
}

// envProxy reads HTTP_PROXY / HTTPS_PROXY / NO_PROXY (and the lower
// case names) now, like curl. Loopback hosts never use a proxy.
func envProxy() func(*http.Request) (*url.URL, error) {
	get := func(names ...string) string {
		for _, n := range names {
			if v := os.Getenv(n); v != "" {
				return v
			}
		}
		return ""
	}
	httpP := get("HTTP_PROXY", "http_proxy")
	httpsP := get("HTTPS_PROXY", "https_proxy")
	noP := get("NO_PROXY", "no_proxy")
	parse := func(s string) *url.URL {
		if s == "" {
			return nil
		}
		if !strings.Contains(s, "://") {
			s = "http://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			return nil
		}
		return u
	}
	hp, hsp := parse(httpP), parse(httpsP)
	var skip []string
	for _, p := range strings.Split(noP, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			skip = append(skip, p)
		}
	}
	return func(r *http.Request) (*url.URL, error) {
		host := strings.ToLower(r.URL.Hostname())
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return nil, nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil, nil
		}
		for _, s := range skip {
			if s == "*" {
				return nil, nil
			}
			s = strings.TrimPrefix(s, "*")
			if h, _, err := net.SplitHostPort(s); err == nil {
				s = h
			}
			if host == strings.TrimPrefix(s, ".") || strings.HasSuffix(host, "."+strings.TrimPrefix(s, ".")) {
				return nil, nil
			}
		}
		if r.URL.Scheme == "https" {
			return hsp, nil
		}
		return hp, nil
	}
}
