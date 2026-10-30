package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// rawServer is an MCP endpoint whose answers the test writes by hand.
// It records every request body it gets.
type rawServer struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []string
	posted chan string
}

func newRawServer(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body string)) *rawServer {
	t.Helper()
	s := &rawServer{posted: make(chan string, 16)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(b))
		s.mu.Unlock()
		if r.Method == http.MethodPost {
			select {
			case s.posted <- string(b):
			default:
			}
		}
		h(w, r, string(b))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func rawConn(t *testing.T, url string, handle func(extproc.Message, replyFunc)) *httpConn {
	t.Helper()
	c := newHTTPConn(url, nil, HTTPOptions{}, handle)
	t.Cleanup(func() { c.end(false) })
	return c
}

func callCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func jsonAnswer(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, s)
}

func sseAnswer(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, s)
}

// Spec (MC-5): a non-2xx answer becomes "HTTP <code>: <short body>"; a
// long body is cut, an empty one leaves only the code.
func TestHTTPStatusErrors(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"short": {500, "  boom  ", "HTTP 500: boom"},
		"empty": {503, "", "HTTP 503"},
		"long":  {502, strings.Repeat("x", 280), "HTTP 502: " + strings.Repeat("x", 200) + "…"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			})
			_, err := rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil)
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if err := rawConn(t, s.srv.URL, nil).Notify("notifications/x", nil); err == nil || err.Error() != c.want {
				t.Fatalf("Notify err = %v, want %q", err, c.want)
			}
		})
	}
}

// Spec (MC-5): JSON answers that are broken, too big, an error, or for
// another id each fail clearly.
func TestHTTPBadJSONAnswers(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"not json":       {`{oops`, "bad answer"},
		"bad batch":      {`[{oops]`, "bad answer"},
		"other id":       {`{"jsonrpc":"2.0","id":99,"result":{}}`, "sent no answer"},
		"error":          {`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no such method"}}`, "no such method"},
		"too big":        {`{"jsonrpc":"2.0","id":1,"result":"` + strings.Repeat("a", extproc.MaxMessage) + `"}`, "too big"},
		"empty response": {``, "bad answer"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, _ string) { jsonAnswer(w, c.body) })
			_, err := rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// Spec (MC-5): a JSON batch answer may carry a server notification
// before the answer; the notification reaches the client.
func TestHTTPBatchAnswerDispatches(t *testing.T) {
	s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		jsonAnswer(w, `[{"jsonrpc":"2.0","method":"notifications/message","params":{"data":"hi"}},{"jsonrpc":"2.0","id":1,"result":{"ok":true}}]`)
	})
	var got []string
	c := rawConn(t, s.srv.URL, func(m extproc.Message, reply replyFunc) {
		got = append(got, m.Method)
		reply(nil, nil) // a notification gets no answer
	})
	res, err := c.Call(callCtx(t), "tools/list", nil)
	if err != nil || string(res) != `{"ok":true}` {
		t.Fatalf("Call = %s %v", res, err)
	}
	if len(got) != 1 || got[0] != "notifications/message" {
		t.Fatalf("dispatched %v", got)
	}
	s.mu.Lock()
	n := len(s.bodies)
	s.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d requests: a notification was answered", n)
	}
}

// Spec (MC-5): a server request inside the SSE stream is answered with
// its own POST (result or error); a connection with no handler skips it.
func TestHTTPServerRequestInStream(t *testing.T) {
	for _, withErr := range []bool{false, true} {
		s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, body string) {
			if strings.Contains(body, `"method":"tools/list"`) {
				sseAnswer(w, ": keep-alive\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"srv-1\",\"method\":\"roots/list\"}\n\n"+
					"data: not json\n\n"+
					"data: {\"jsonrpc\":\"2.0\",\n"+"data: \"id\":1,\"result\":{\"tools\":[]}}\n\n")
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})
		c := rawConn(t, s.srv.URL, func(m extproc.Message, reply replyFunc) {
			if withErr {
				reply(nil, &extproc.Error{Code: -32601, Message: "nope"})
			} else {
				reply(map[string]any{"roots": []any{}}, nil)
			}
		})
		res, err := c.Call(callCtx(t), "tools/list", nil)
		if err != nil || string(res) != `{"tools":[]}` {
			t.Fatalf("Call = %s %v", res, err)
		}
		<-s.posted // the call itself
		select {
		case b := <-s.posted:
			var m map[string]any
			_ = json.Unmarshal([]byte(b), &m)
			_, hasErr := m["error"]
			_, hasRes := m["result"]
			if m["id"] != "srv-1" || hasErr != withErr || hasRes == withErr {
				t.Fatalf("reply %s", b)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the server request got no answer")
		}
	}
	s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		sseAnswer(w, "data: {\"jsonrpc\":\"2.0\",\"id\":\"srv-1\",\"method\":\"roots/list\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
	})
	if _, err := rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil); err != nil {
		t.Fatalf("no handler: %v", err)
	}
}

// Spec (MC-5): a stream that ends with no answer and no event id fails
// at once; one whose resume is refused says it could not be resumed.
func TestHTTPStreamEndsWithoutAnswer(t *testing.T) {
	s := newRawServer(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		sseAnswer(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
	})
	_, err := rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil)
	if err == nil || !strings.Contains(err.Error(), "ended the stream without an answer") {
		t.Fatalf("err = %v", err)
	}

	s = newRawServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		sseAnswer(w, "id: ev-1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
	})
	_, err = rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil)
	if err == nil || !strings.Contains(err.Error(), "could not be resumed: HTTP 405") {
		t.Fatalf("err = %v", err)
	}
}

// Spec (MC-5): a stream is resumed at most maxResumes times.
func TestHTTPStreamResumeCap(t *testing.T) {
	var gets int
	var mu sync.Mutex
	s := newRawServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.Method == http.MethodGet {
			mu.Lock()
			gets++
			mu.Unlock()
		}
		sseAnswer(w, "id: ev\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
	})
	_, err := rawConn(t, s.srv.URL, nil).Call(callCtx(t), "tools/list", nil)
	if err == nil || !strings.Contains(err.Error(), "without an answer") {
		t.Fatalf("err = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if gets != maxResumes {
		t.Fatalf("%d resumes, want %d", gets, maxResumes)
	}
}

// Spec (MC-5): end cancels the connection before it marks it done; a
// call cut in that window still ends with ErrClosed, not "context
// canceled".
func TestHTTPCallCutWhileClosing(t *testing.T) {
	s := newRawServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		<-r.Context().Done()
	})
	c := newHTTPConn(s.srv.URL, nil, HTTPOptions{}, nil)
	defer c.end(false)
	c.cancel() // the first half of end: done is not closed yet
	if _, err := c.Call(context.Background(), "tools/list", nil); !errors.Is(err, extproc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
}

// Spec (MC-5): a closed connection refuses calls and notifications; a
// Close during a call ends it with ErrClosed.
func TestHTTPClosedConnection(t *testing.T) {
	release := make(chan struct{})
	s := newRawServer(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	c := newHTTPConn(s.srv.URL, nil, HTTPOptions{}, nil)
	errc := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "tools/list", nil)
		errc <- err
	}()
	<-s.posted
	c.end(false)
	select {
	case err := <-errc:
		if !errors.Is(err, extproc.ErrClosed) {
			t.Fatalf("in-flight call: err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end the call")
	}
	if _, err := c.Call(context.Background(), "tools/list", nil); !errors.Is(err, extproc.ErrClosed) {
		t.Fatalf("Call after close: %v", err)
	}
	if err := c.Notify("notifications/x", nil); !errors.Is(err, extproc.ErrClosed) {
		t.Fatalf("Notify after close: %v", err)
	}
}

// Spec (MC-5): bad URLs are refused before any request.
func TestHTTPBadURLs(t *testing.T) {
	for raw, want := range map[string]string{
		"/no-host":                 "invalid url",
		"ftp://files.example/mcp":  "invalid url scheme",
		"http://remote.example/mc": "insecure url",
	} {
		if _, err := StartHTTP(context.Background(), Server{Name: "x", Type: TypeHTTP, URL: raw}, HTTPOptions{}, Options{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
	c := rawConn(t, "http://127.0.0.1:1/\x7f", nil)
	if _, err := c.Call(callCtx(t), "tools/list", nil); err == nil {
		t.Fatal("a bad URL made a request")
	}
	if got := hostOf("::bad"); got != "server" {
		t.Fatalf("hostOf = %q", got)
	}
	if err := unwrapURLError(io.EOF); err != io.EOF {
		t.Fatalf("unwrapURLError changed a plain error: %v", err)
	}
}

// Spec (MC-6): the WWW-Authenticate parser reads quoted values with
// escapes and commas, bare values, and stops at junk.
func TestAuthParamsEdgeCases(t *testing.T) {
	got := authParams(`Bearer error_description="say \"hi\", then go", scope=read write,resource_metadata="https://x.example/prm"`)
	if got["error_description"] != `say "hi", then go` || got["scope"] != "read write" || got["resource_metadata"] != "https://x.example/prm" {
		t.Fatalf("params %v", got)
	}
	if got := authParams(`Bearer realm="x", junk`); got["realm"] != "x" || len(got) != 1 {
		t.Fatalf("junk: %v", got)
	}
	if got := authParams(`Bearer error="unterminated`); got["error"] != "unterminated" {
		t.Fatalf("unterminated: %v", got)
	}
	if got := authParams(`Bearer scope=last`); got["scope"] != "last" {
		t.Fatalf("bare last: %v", got)
	}
	ae := parseAuthenticate(`Bearer error="invalid_token"`)
	if ae.Detail != "invalid_token" || ae.Error() != "not authorized (HTTP 401): invalid_token" {
		t.Fatalf("auth error %+v %q", ae, ae.Error())
	}
}
