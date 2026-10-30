package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func jsonRun(body string) *httpRun {
	return &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: []byte(body), Size: int64(len(body)), IsJSON: true}}
}

// PERF-8: the Body view's lines are worked out once per answer; Tab
// back to Body reuses them. A new answer (a new Response) is laid out
// again.
func TestHTTPBodyLinesCachedPerResponse(t *testing.T) {
	run := jsonRun(`{"a":1}`)
	p := buildHTTPPage(run, httpViewBody, false, false)
	if got := strings.Join(p.lines[3:], "\n"); got != "{\n  \"a\": 1\n}" {
		t.Fatalf("body %q", got)
	}
	if run.body == nil || run.body.resp != run.resp {
		t.Fatal("no cached body lines")
	}
	run.body.lines[1] = "  cached"
	if p := buildHTTPPage(run, httpViewBody, false, false); p.lines[4] != "  cached" {
		t.Fatalf("second build did not use the cache: %q", p.lines[4])
	}
	if p := buildHTTPPage(run, httpViewRaw, false, false); p.lines[3] != `{"a":1}` {
		t.Fatalf("raw view: %q", p.lines[3])
	}
	// A new answer for the same run.
	run.resp = &httpclient.Response{Status: 200, Body: []byte(`[2]`), Size: 3, IsJSON: true}
	if got := strings.Join(buildHTTPPage(run, httpViewBody, false, false).lines[3:], "\n"); got != "[\n  2\n]" {
		t.Fatalf("stale body after a new answer: %q", got)
	}
}

// Old runs whose body is cut in the ring drop their cached lines.
func TestHTTPRingDropsBodyLines(t *testing.T) {
	s := newHTTPState()
	big := `"` + strings.Repeat("x", 70<<10) + `"`
	var first *httpRun
	for i := 0; i < httpRingBodies+2; i++ {
		r := jsonRun("[" + big + "]")
		buildHTTPPage(r, httpViewBody, false, false)
		if first == nil {
			first = r
		}
		s.remember(r)
	}
	if !first.resp.Truncated || first.body != nil {
		t.Fatalf("truncated %v, cached lines kept %v", first.resp.Truncated, first.body != nil)
	}
}

// The send goroutine lays the body out, so Update only shows it.
func TestHTTPSendLaysOutBodyOffTheUILoop(t *testing.T) {
	withConfigDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	m := &Model{}
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: srv.URL}}}
	cmd := m.startHTTPRun(run, httpclient.Hooks{})
	var ev httpEventMsg
	for {
		msg, ok := cmd().(httpEventMsg)
		if !ok {
			t.Fatal("no event")
		}
		if msg.ev.resp != nil {
			ev = msg
			break
		}
		cmd = waitHTTPEvent(msg.gen, msg.run, msg.ch)
	}
	if ev.ev.body == nil || ev.ev.body.resp != ev.ev.resp || strings.Join(ev.ev.body.lines, "\n") != "{\n  \"ok\": true\n}" {
		t.Fatalf("event body %+v", ev.ev.body)
	}
	m.handleHTTPMsg(ev)
	if run.body != ev.ev.body {
		t.Fatal("the run did not keep the laid-out body")
	}
}
