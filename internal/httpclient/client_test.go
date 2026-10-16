package httpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func send(t *testing.T, c *Client, method, url string, body string, h Hooks, headers ...Header) *Response {
	t.Helper()
	r := Request{Method: method, URL: url, Headers: headers}
	if body != "" {
		r.Body = &body
	}
	return c.Send(context.Background(), Resolved{Request: r, Template: r}, h)
}

func TestSendJSON(t *testing.T) {
	var gotCT, gotUA, gotLen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT, gotUA, gotLen = r.Header.Get("Content-Type"), r.Header.Get("User-Agent"), r.Header.Get("Content-Length")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Request-Id", "8f1c")
		w.WriteHeader(201)
		io.WriteString(w, `{"id":42,"name":"Ada"}`)
	}))
	defer srv.Close()
	c := New(Options{UserAgent: "termocode/test"})
	resp := send(t, c, "POST", srv.URL+"/users", `  {"name":"Ada"}`, Hooks{})
	if resp.Err != "" {
		t.Fatal(resp.Err)
	}
	if resp.Status != 201 || resp.StatusText != "Created" || resp.Proto != "HTTP/1.1" {
		t.Fatalf("status %d %q %q", resp.Status, resp.StatusText, resp.Proto)
	}
	if !resp.IsJSON || resp.Charset != "utf-8" || resp.ContentType != "application/json" {
		t.Fatalf("type %q %q json=%v", resp.ContentType, resp.Charset, resp.IsJSON)
	}
	if gotCT != "application/json" {
		t.Fatalf("auto content type = %q", gotCT)
	}
	if gotUA != "termocode/test" || gotLen != "16" {
		t.Fatalf("ua %q len %q", gotUA, gotLen)
	}
	if resp.Header("x-request-id") != "8f1c" {
		t.Fatalf("headers lower-cased: %v", resp.Headers)
	}
	pretty, ok := PrettyJSON(resp.Body)
	if !ok || string(pretty) != "{\n  \"id\": 42,\n  \"name\": \"Ada\"\n}" {
		t.Fatalf("pretty %q", pretty)
	}
	if resp.Size != 22 || resp.Elapsed <= 0 {
		t.Fatalf("size %d elapsed %v", resp.Size, resp.Elapsed)
	}
	if len(resp.ReqHeaders) == 0 {
		t.Fatal("request headers must be recorded")
	}
}

func TestSendKeepsGivenContentType(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Content-Type") }))
	defer srv.Close()
	send(t, New(Options{}), "POST", srv.URL, `{"a":1}`, Hooks{}, Header{"content-type", "text/plain"})
	if got != "text/plain" {
		t.Fatalf("content type = %q", got)
	}
	send(t, New(Options{}), "POST", srv.URL, `a=1`, Hooks{})
	if got != "" {
		t.Fatalf("non-JSON body gets no content type, got %q", got)
	}
}

func TestSendGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		zw.Write([]byte("hello gzip"))
		zw.Close()
	}))
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL, "", Hooks{})
	if string(resp.Body) != "hello gzip" {
		t.Fatalf("body %q (%s)", resp.Body, resp.Err)
	}
}

func TestRedirectHops(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/mid", 301) })
	mux.HandleFunc("/mid", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/new", 307) })
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL+"/old", "", Hooks{})
	if resp.Status != 200 || len(resp.Hops) != 2 {
		t.Fatalf("status %d hops %+v err %s", resp.Status, resp.Hops, resp.Err)
	}
	if resp.Hops[0].Status != 301 || resp.Hops[0].URL != srv.URL+"/mid" || resp.Hops[1].Status != 307 || resp.Hops[1].URL != srv.URL+"/new" {
		t.Fatalf("hops %+v", resp.Hops)
	}
	if resp.URL != srv.URL+"/new" {
		t.Fatalf("final URL %q", resp.URL)
	}
	// @no-redirect: the 3xx is the response.
	r := Request{Method: "GET", URL: srv.URL + "/old", Directives: map[string]string{"no-redirect": ""}}
	resp = New(Options{}).Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.Status != 301 || len(resp.Hops) != 0 {
		t.Fatalf("no-redirect: %d %+v", resp.Status, resp.Hops)
	}
}

func TestTooManyRedirects(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("/r%d", n.Add(1)), 302)
	}))
	defer srv.Close()
	resp := send(t, New(Options{MaxRedirects: 10}), "GET", srv.URL, "", Hooks{})
	if resp.ErrKind != ErrKindRedirects || resp.Err != "stopped after 10 redirects" || len(resp.Hops) != 10 {
		t.Fatalf("kind %v err %q hops %d", resp.ErrKind, resp.Err, len(resp.Hops))
	}
}

func TestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	resp := send(t, New(Options{Timeout: 200 * time.Millisecond}), "GET", srv.URL, "", Hooks{})
	if resp.ErrKind != ErrKindTimeout || resp.Err != "timed out after 200 ms" {
		t.Fatalf("kind %v err %q", resp.ErrKind, resp.Err)
	}
	if resp.Elapsed < 200*time.Millisecond {
		t.Fatalf("elapsed %v", resp.Elapsed)
	}
	// # @timeout overrides the option.
	r := Request{Method: "GET", URL: srv.URL, Directives: map[string]string{"timeout": "100ms"}}
	resp = New(Options{Timeout: 10 * time.Second}).Send(context.Background(), Resolved{Request: r}, Hooks{})
	if resp.ErrKind != ErrKindTimeout || resp.Elapsed > 5*time.Second {
		t.Fatalf("directive timeout: %v %v", resp.ErrKind, resp.Elapsed)
	}
}

func TestCancelMidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 300<<10))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var progressed atomic.Bool
	hooks := Hooks{Progress: func(n int64) {
		if n >= 256<<10 && progressed.CompareAndSwap(false, true) {
			cancel()
		}
	}}
	r := Request{Method: "GET", URL: srv.URL}
	resp := New(Options{}).Send(ctx, Resolved{Request: r}, hooks)
	if resp.ErrKind != ErrKindCancelled || !strings.HasPrefix(resp.Err, "cancelled after") {
		t.Fatalf("kind %v err %q", resp.ErrKind, resp.Err)
	}
	if !progressed.Load() {
		t.Fatal("progress hook never fired")
	}
}

func TestBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("y"), 1<<20)
		for i := 0; i < 30; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	resp := send(t, New(Options{MaxBody: 1 << 20}), "GET", srv.URL, "", Hooks{})
	if !resp.Truncated || len(resp.Body) != 1<<20 || resp.Err != "" {
		t.Fatalf("truncated %v len %d err %q", resp.Truncated, len(resp.Body), resp.Err)
	}
}

func TestTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secure") }))
	defer srv.Close()
	c := New(Options{})
	resp := send(t, c, "GET", srv.URL, "", Hooks{})
	if resp.ErrKind != ErrKindTLS || !strings.HasPrefix(resp.Err, "TLS: ") {
		t.Fatalf("kind %v err %q", resp.ErrKind, resp.Err)
	}
	resp = send(t, c, "GET", srv.URL, "", Hooks{Insecure: true})
	if resp.Status != 200 || string(resp.Body) != "secure" {
		t.Fatalf("insecure retry: %d %q %s", resp.Status, resp.Body, resp.Err)
	}
	// The retry is per send: the next normal send checks again.
	if resp = send(t, c, "GET", srv.URL, "", Hooks{}); resp.ErrKind != ErrKindTLS {
		t.Fatalf("TLS check must come back, got %v", resp.ErrKind)
	}
}

func TestCookieJar(t *testing.T) {
	var seen atomic.Value
	seen.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "s3cr3t-session", Path: "/"})
			return
		}
		seen.Store(r.Header.Get("Cookie"))
	}))
	defer srv.Close()
	c := New(Options{CookieJar: true})
	send(t, c, "GET", srv.URL+"/login", "", Hooks{})
	send(t, c, "GET", srv.URL+"/me", "", Hooks{})
	if seen.Load() != "session=s3cr3t-session" {
		t.Fatalf("cookie not sent back: %q", seen.Load())
	}
	r := Request{Method: "GET", URL: srv.URL + "/me", Directives: map[string]string{"no-cookie-jar": ""}}
	c.Send(context.Background(), Resolved{Request: r}, Hooks{})
	if seen.Load() != "" {
		t.Fatalf("@no-cookie-jar still sent %q", seen.Load())
	}
	c2 := New(Options{CookieJar: false})
	send(t, c2, "GET", srv.URL+"/login", "", Hooks{})
	send(t, c2, "GET", srv.URL+"/me", "", Hooks{})
	if seen.Load() != "" {
		t.Fatalf("jar off still sent %q", seen.Load())
	}
}

func TestLatin1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=ISO-8859-1")
		w.Write([]byte{'c', 'a', 'f', 0xe9})
	}))
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL, "", Hooks{})
	if resp.Charset != "iso-8859-1" || resp.Text() != "café" {
		t.Fatalf("charset %q text %q", resp.Charset, resp.Text())
	}
}

func TestBinary(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nul" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("ab\x00cd"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL, "", Hooks{})
	if !resp.IsBinary || resp.ContentType != "image/png" {
		t.Fatalf("binary %v type %q", resp.IsBinary, resp.ContentType)
	}
	resp = send(t, New(Options{}), "GET", srv.URL+"/nul", "", Hooks{})
	if !resp.IsBinary {
		t.Fatal("a NUL byte makes the body binary")
	}
}

func TestAllowIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "hi") }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	resolver := func(ctx context.Context, host string) ([]net.IP, error) {
		switch host {
		case "evil.test":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		case "metadata.test":
			return []net.IP{net.ParseIP("169.254.169.254")}, nil
		}
		return nil, fmt.Errorf("no such host %s", host)
	}
	c := New(Options{Resolve: resolver})
	notPrivate := func(ip net.IP) bool { return !IsPrivateIP(ip) }
	resp := send(t, c, "GET", "http://evil.test:"+port+"/", "", Hooks{AllowIP: notPrivate})
	if resp.Status != 200 {
		t.Fatalf("loopback is allowed: %d %s", resp.Status, resp.Err)
	}
	resp = send(t, c, "GET", "http://metadata.test:"+port+"/", "", Hooks{AllowIP: notPrivate})
	if resp.ErrKind != ErrKindBlocked || !strings.Contains(resp.Err, "blocked: private address") {
		t.Fatalf("metadata must be blocked: %v %q", resp.ErrKind, resp.Err)
	}
	resp = send(t, c, "GET", srv.URL, "", Hooks{AllowIP: func(ip net.IP) bool { return !ip.IsLoopback() }})
	if resp.ErrKind != ErrKindBlocked {
		t.Fatalf("AllowIP refusing 127.0.0.1: %v %q", resp.ErrKind, resp.Err)
	}
}

func TestCheckHop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://other.test/x", 302) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL+"/a", "", Hooks{CheckHop: func(u string) error {
		return fmt.Errorf("redirect to %s needs a new approval", u)
	}})
	if resp.ErrKind != ErrKindBlocked || !strings.Contains(resp.Err, "other.test") {
		t.Fatalf("hop not re-gated: %v %q", resp.ErrKind, resp.Err)
	}
}

func TestProxyFromEnv(t *testing.T) {
	var seen atomic.Value
	seen.Store("")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.URL.String())
		io.WriteString(w, "via proxy")
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", "")
	t.Setenv("NO_PROXY", "skip.test")
	t.Setenv("no_proxy", "")
	resp := send(t, New(Options{}), "GET", "http://api.example.test/users?x=1", "", Hooks{})
	if string(resp.Body) != "via proxy" || seen.Load() != "http://api.example.test/users?x=1" {
		t.Fatalf("proxy saw %q body %q err %s", seen.Load(), resp.Body, resp.Err)
	}
	if p, _ := envProxy()(&http.Request{URL: mustURL("http://a.skip.test/")}); p != nil {
		t.Fatalf("NO_PROXY ignored: %v", p)
	}
}

func TestDialErrorAndBadScheme(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	resp := send(t, New(Options{}), "GET", "http://"+addr, "", Hooks{})
	if resp.ErrKind != ErrKindDial || resp.Err == "" || resp.Status != 0 {
		t.Fatalf("dial: %v %q", resp.ErrKind, resp.Err)
	}
	resp = send(t, New(Options{}), "GET", "ftp://x", "", Hooks{})
	if resp.Err == "" {
		t.Fatal("bad scheme must fail")
	}
}

func TestTimingRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	resp := send(t, New(Options{}), "GET", srv.URL, "", Hooks{})
	if resp.Timing.Total <= 0 || resp.Timing.FirstByte <= 0 || resp.Timing.Connect <= 0 {
		t.Fatalf("timing %+v", resp.Timing)
	}
}

func BenchmarkPrettyJSON5MB(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; sb.Len() < 5<<20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":%d,"name":"user %d","tags":["a","b"],"address":{"city":"x","zip":"%05d"}}`, i, i, i)
	}
	sb.WriteString("]")
	data := []byte(sb.String())
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := PrettyJSON(data); !ok {
			b.Fatal("not json")
		}
	}
}

func BenchmarkSend1000(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	c := New(Options{})
	r := Request{Method: "GET", URL: srv.URL}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 1000; j++ {
			if resp := c.Send(context.Background(), Resolved{Request: r}, Hooks{}); resp.Status != 200 {
				b.Fatal(resp.Err)
			}
		}
	}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
