package testkit

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// SCN-25: the HTTP client when things go wrong, through the real app
// and nvim: the server is offline, a redirect loop never ends, and an
// answer is bigger than the http_max_body_mb limit. Every request goes
// to 127.0.0.1: no network.

// httpEdgeServer answers /loop with a redirect to itself and /huge
// with 2 MB of text.
func httpEdgeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop?n="+r.URL.Query().Get("n")+"x", http.StatusFound)
		case "/huge":
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, strings.Repeat("0123456789abcdef\n", (2<<20)/17+1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// closedPort is a 127.0.0.1 port nobody listens on.
func closedPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return strconv.Itoa(port)
}

// httpEdgeDriver opens api/edge.http with one request per line 1, 4, 7.
func httpEdgeDriver(t *testing.T, offline, live, config string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	files := textFiles()
	files["api/edge.http"] = "GET http://127.0.0.1:" + offline + "/ping\n" +
		"\n" +
		"###\n" +
		"GET " + live + "/loop\n" +
		"\n" +
		"###\n" +
		"GET " + live + "/huge\n"
	opt := Options{W: 110, H: 32, Nvim: true, Files: files}
	if config != "" {
		opt.Config = map[string]string{"config.json": config}
	}
	d := New(t, opt)
	open(d, "api/edge.http")
	return d
}

// Offline: a refused connection shows the error row and the connect
// help, and r resends (still refused, no crash).
func TestHTTPOfflineServer(t *testing.T) {
	srv := httpEdgeServer(t)
	d := httpEdgeDriver(t, closedPort(t), srv.URL, "")
	httpCursor(d, 1)
	d.Palette("http-send")
	waitPage(d, "Could not connect. Check the host and the port")
	if p := httpPageText(d); !strings.Contains(p, "refused") || !strings.Contains(p, "r   resend") {
		t.Fatalf("offline page:\n%s", p)
	}
	httpFocusPage(d)
	d.Key("r")
	waitPage(d, "Could not connect.")
}

// A redirect loop stops at http_max_redirects with its own help row.
func TestHTTPTooManyRedirects(t *testing.T) {
	srv := httpEdgeServer(t)
	d := httpEdgeDriver(t, closedPort(t), srv.URL, `{"http_max_redirects":3}`)
	httpCursor(d, 4)
	d.Palette("http-send")
	waitPage(d, "stopped after 3 redirects")
	if p := httpPageText(d); !strings.Contains(p, "Too many redirects (setting http_max_redirects).") {
		t.Fatalf("redirect help missing:\n%s", p)
	}
}

// A body over http_max_body_mb is cut at the limit and the summary row
// says so; the page still opens and shows the start of the body.
func TestHTTPLargeBodyTruncated(t *testing.T) {
	srv := httpEdgeServer(t)
	d := httpEdgeDriver(t, closedPort(t), srv.URL, `{"http_max_body_mb":1}`)
	httpCursor(d, 7)
	d.Palette("http-send")
	waitPage(d, "truncated at 1.0 MB")
	p := httpPageText(d)
	if !strings.Contains(p, "200 OK") || !strings.Contains(p, "0123456789abcdef") {
		t.Fatalf("large body page:\n%s", p[:min(len(p), 2000)])
	}
}
