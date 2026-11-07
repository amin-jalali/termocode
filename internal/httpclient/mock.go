package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// MockRoute is one request block served by a Mock: its method, path
// pattern and canned answer from the `# @mock-*` directives.
type MockRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"` // `{{x}}` and `:x` segments match any segment
	Status int    `json:"status"`
	Type   string `json:"type,omitempty"`
	Body   string `json:"body,omitempty"`
	Line   int    `json:"line"`
}

// Mock is a local mock server built from a .http file (API-22). It only
// listens on 127.0.0.1, on a random port.
type Mock struct {
	routes []MockRoute
	srv    *http.Server
	done   chan struct{}
	once   sync.Once
}

// NewMock builds the routes of f. Each request block answers with
// `# @mock-status` (default 200), `# @mock-type` (default JSON when the
// body looks like JSON, else text) and `# @mock-body`.
func NewMock(f File) (*Mock, error) {
	if len(f.Requests) == 0 {
		return nil, errors.New("the file has no requests to mock")
	}
	m := &Mock{done: make(chan struct{})}
	for _, r := range f.Requests {
		rt := MockRoute{Method: strings.ToUpper(r.Method), Path: mockPathOf(r.URL), Status: http.StatusOK, Line: r.Line}
		if v, ok := r.Directives["mock-status"]; ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 100 || n > 999 {
				return nil, fmt.Errorf("line %d: bad @mock-status %q", r.Line, v)
			}
			rt.Status = n
		}
		rt.Body = r.Directives["mock-body"]
		rt.Type = r.Directives["mock-type"]
		if rt.Type == "" && rt.Body != "" {
			if b := strings.TrimSpace(rt.Body); strings.HasPrefix(b, "{") || strings.HasPrefix(b, "[") {
				rt.Type = "application/json"
			} else {
				rt.Type = "text/plain; charset=utf-8"
			}
		}
		m.routes = append(m.routes, rt)
	}
	return m, nil
}

// Routes are the served routes, in file order.
func (m *Mock) Routes() []MockRoute { return append([]MockRoute(nil), m.routes...) }

// mockPathOf is the path of a request URL: no scheme, host, leading
// `{{base}}` variable, query or fragment.
func mockPathOf(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.IndexByte(u, '/'); j >= 0 {
			u = u[j:]
		} else {
			u = ""
		}
	} else if strings.HasPrefix(u, "{{") {
		if j := strings.Index(u, "}}"); j >= 0 {
			u = u[j+2:]
		}
	}
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return u
}

func mockSegs(p string) []string { return strings.Split(strings.Trim(p, "/"), "/") }

func (rt MockRoute) matches(method, path string) bool {
	if rt.Method != method {
		return false
	}
	want, got := mockSegs(rt.Path), mockSegs(path)
	if len(want) != len(got) {
		return false
	}
	for i, w := range want {
		if strings.HasPrefix(w, "{{") && strings.HasSuffix(w, "}}") || strings.HasPrefix(w, ":") && len(w) > 1 {
			continue
		}
		if w != got[i] {
			return false
		}
	}
	return true
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rt := range m.routes {
		if !rt.matches(r.Method, r.URL.Path) {
			continue
		}
		if rt.Type != "" {
			w.Header().Set("Content-Type", rt.Type)
		}
		w.WriteHeader(rt.Status)
		_, _ = w.Write([]byte(rt.Body))
		return
	}
	http.Error(w, "no mock for "+r.Method+" "+r.URL.Path, http.StatusNotFound)
}

// Start listens on a random 127.0.0.1 port and serves until ctx ends. It
// returns the host:port.
func (m *Mock) Start(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	m.srv = &http.Server{Handler: m, ReadHeaderTimeout: 10 * time.Second}
	crash.Go("httpclient.mock.serve", func() {
		defer m.once.Do(func() { close(m.done) })
		_ = m.srv.Serve(ln)
	})
	crash.Go("httpclient.mock.stop", func() {
		select {
		case <-ctx.Done():
			sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = m.srv.Shutdown(sctx)
		case <-m.done:
		}
	})
	return ln.Addr().String(), nil
}

// Wait blocks until the server stopped.
func (m *Mock) Wait() {
	if m.srv != nil {
		<-m.done
	}
}
