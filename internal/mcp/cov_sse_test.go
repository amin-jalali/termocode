package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// covSSE is a small old-transport server: GET /sse is the event stream,
// POST /msg takes the client's messages. Answers go back on the stream.
type covSSE struct {
	*httptest.Server
	mu      sync.Mutex
	posts   []map[string]any
	headers []http.Header
	out     chan string // raw events to write to the stream
	postErr int         // status of every POST (0: 202)
	respond bool        // answer requests on the stream
}

func newCovSSE(t *testing.T, first string) *covSSE {
	s := &covSSE{out: make(chan string, 16), respond: true}
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.headers = append(s.headers, r.Header.Clone())
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fmt.Fprint(w, first)
		fl.Flush()
		for {
			select {
			case ev := <-s.out:
				fmt.Fprint(w, ev)
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/msg", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		s.mu.Lock()
		s.posts = append(s.posts, m)
		s.headers = append(s.headers, r.Header.Clone())
		code, respond := s.postErr, s.respond
		s.mu.Unlock()
		if code != 0 {
			if code == 401 {
				w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://x/meta", scope="a b"`)
			}
			http.Error(w, "no", code)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		if _, isReq := m["method"]; isReq && respond && m["id"] != nil {
			s.out <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%v,\"result\":{\"echo\":%q}}\n\n", m["id"], m["method"])
		}
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *covSSE) lastPost(t *testing.T) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.posts)
		var last map[string]any
		if n > 0 {
			last = s.posts[n-1]
		}
		s.mu.Unlock()
		if last != nil {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the server got no POST")
	return nil
}

func TestCovLegacySSEConversation(t *testing.T) {
	s := newCovSSE(t, ": hello\n\nevent: endpoint\ndata: /msg\n\n")
	got := make(chan extproc.Message, 4)
	conn, err := dialLegacy(context.Background(), s.URL+"/sse", map[string]string{"X-Test": "yes"}, HTTPOptions{
		Token: func(context.Context) string { return "tok" },
	}, func(m extproc.Message, reply replyFunc) {
		got <- m
		reply(map[string]any{"ok": true}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.endpoint != s.URL+"/msg" {
		t.Errorf("endpoint %q", conn.endpoint)
	}
	// the headers of the entry and the bearer token reach the stream and the posts
	res, err := conn.Call(context.Background(), "tools/list", map[string]any{"a": 1})
	if err != nil || !strings.Contains(string(res), "tools/list") {
		t.Fatalf("call %s %v", res, err)
	}
	s.mu.Lock()
	h := s.headers[len(s.headers)-1]
	s.mu.Unlock()
	if h.Get("X-Test") != "yes" || h.Get("Authorization") != "Bearer tok" || h.Get("Content-Type") != "application/json" {
		t.Errorf("post headers %v", h)
	}
	if err := conn.Notify("notifications/x", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if p := s.lastPost(t); p["method"] == nil {
		t.Errorf("last post %v", p)
	}
	// a request from the server is answered with a POST; a notification is not
	s.out <- "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"srv-1\",\"method\":\"ping\"}\n\n"
	s.out <- "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n"
	s.out <- "data: {broken\n\n"
	s.out <- "event: other\ndata: ignored\n\n"
	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(5 * time.Second):
			t.Fatal("the server's messages did not arrive")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		var reply map[string]any
		for _, p := range s.posts {
			if p["id"] == "srv-1" {
				reply = p
			}
		}
		s.mu.Unlock()
		if reply != nil {
			if reply["result"] == nil {
				t.Errorf("reply %v", reply)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer to the server's request was not posted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// a multi-line data field joins with a newline
	s.out <- "data: {\"jsonrpc\":\"2.0\",\ndata: \"method\":\"multi\"}\n\n"
	select {
	case m := <-got:
		if m.Method != "multi" {
			t.Errorf("multi-line %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("multi-line message lost")
	}
}

func TestCovLegacySSECallFailures(t *testing.T) {
	s := newCovSSE(t, "event: endpoint\ndata: /msg\n\n")
	conn, err := dialLegacy(context.Background(), s.URL+"/sse", nil, HTTPOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// a server request with no handler is dropped quietly
	s.out <- "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n\n"
	// a POST the server refuses
	s.mu.Lock()
	s.postErr = 500
	s.mu.Unlock()
	if _, err := conn.Call(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("500: %v", err)
	}
	s.mu.Lock()
	s.postErr = 401
	s.mu.Unlock()
	err = conn.Notify("x", nil)
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Errorf("401 must be an AuthError, got %v", err)
	}
	// a cancelled call tells the server and returns the context error
	s.mu.Lock()
	s.postErr, s.respond = 0, false
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			s.mu.Lock()
			seen := false
			for _, p := range s.posts {
				seen = seen || p["method"] == "slow"
			}
			s.mu.Unlock()
			if seen {
				break
			}
		}
		cancel()
	}()
	if _, err := conn.Call(ctx, "slow", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		found := false
		for _, p := range s.posts {
			found = found || p["method"] == "notifications/cancelled"
		}
		s.mu.Unlock()
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server was not told about the cancel")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// a context that is already over when the post fails returns its error
	dead, stop := context.WithCancel(context.Background())
	stop()
	if _, err := conn.Call(dead, "x", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("dead context: %v", err)
	}
	// closing the connection ends a waiting call and later calls fail
	waiting := make(chan error, 1)
	go func() { _, err := conn.Call(context.Background(), "wait", nil); waiting <- err }()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		seen := false
		for _, p := range s.posts {
			seen = seen || p["method"] == "wait"
		}
		s.mu.Unlock()
		if seen {
			break
		}
	}
	conn.Close()
	select {
	case err := <-waiting:
		if !errors.Is(err, extproc.ErrClosed) {
			t.Errorf("close while waiting: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a call kept waiting after Close")
	}
	if _, err := conn.Call(context.Background(), "x", nil); !errors.Is(err, extproc.ErrClosed) {
		t.Errorf("call after close: %v", err)
	}
	if err := conn.Notify("x", nil); !errors.Is(err, extproc.ErrClosed) {
		t.Errorf("notify after close: %v", err)
	}
}

func TestCovLegacySSEDialErrors(t *testing.T) {
	status := func(code int, ctype, www string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ctype != "" {
				w.Header().Set("Content-Type", ctype)
			}
			if www != "" {
				w.Header().Set("WWW-Authenticate", www)
			}
			w.WriteHeader(code)
		}))
	}
	s401 := status(401, "", `Bearer scope="x"`)
	defer s401.Close()
	var ae *AuthError
	if _, err := dialLegacy(context.Background(), s401.URL, nil, HTTPOptions{}, nil); !errors.As(err, &ae) {
		t.Errorf("401: %v", err)
	}
	s404 := status(404, "text/plain", "")
	defer s404.Close()
	if _, err := dialLegacy(context.Background(), s404.URL, nil, HTTPOptions{}, nil); err == nil || !strings.Contains(err.Error(), "not an MCP server") {
		t.Errorf("404: %v", err)
	}
	html := status(200, "text/html", "")
	defer html.Close()
	if _, err := dialLegacy(context.Background(), html.URL, nil, HTTPOptions{}, nil); err == nil || !strings.Contains(err.Error(), "no event stream") {
		t.Errorf("html: %v", err)
	}
	// the stream ends before an endpoint
	none := newCovSSE(t, ": nothing\n\n")
	_ = none
	end := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": bye\n\n")
	}))
	defer end.Close()
	if _, err := dialLegacy(context.Background(), end.URL, nil, HTTPOptions{}, nil); err == nil || !strings.Contains(err.Error(), "before it named its endpoint") {
		t.Errorf("ended: %v", err)
	}
	// an endpoint on another host
	evil := newCovSSE(t, "event: endpoint\ndata: http://evil.example/msg\n\n")
	if _, err := dialLegacy(context.Background(), evil.URL+"/sse", nil, HTTPOptions{}, nil); err == nil || !strings.Contains(err.Error(), "another host") {
		t.Errorf("evil endpoint: %v", err)
	}
	// nothing listens
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	if _, err := dialLegacy(context.Background(), url, nil, HTTPOptions{}, nil); err == nil {
		t.Error("a closed port must fail")
	}
	// a stream that names no endpoint, with the caller giving up
	silent := newCovSSE(t, ": waiting\n\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := dialLegacy(ctx, silent.URL+"/sse", nil, HTTPOptions{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("silent stream: %v", err)
	}
	// a bad URL
	if _, err := dialLegacy(context.Background(), "http://[::1", nil, HTTPOptions{}, nil); err == nil {
		t.Error("a bad url must fail")
	}
	if _, err := legacyEndpoint("http://[::1", "/x"); err == nil {
		t.Error("a bad base must fail")
	}
	if _, err := legacyEndpoint("http://a/", "http://[::1"); err == nil {
		t.Error("a bad endpoint must fail")
	}
}
