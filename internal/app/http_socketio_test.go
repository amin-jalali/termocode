package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func httpSIOResolved(url, body string, dirs map[string]string) httpclient.Resolved {
	r := httpclient.Request{Method: "SOCKETIO", URL: url, Directives: dirs}
	if body != "" {
		r.Body = &body
	}
	return httpclient.Resolved{Request: r}
}

// Only the SOCKETIO method is Socket.IO; http:// URLs stay HTTP.
func TestIsSocketIORequest(t *testing.T) {
	for _, m := range []string{"SOCKETIO", "socket.io"} {
		if !isSocketIORequest(httpclient.Request{Method: m, URL: "http://x"}) {
			t.Errorf("%s: not Socket.IO", m)
		}
	}
	if isSocketIORequest(httpclient.Request{Method: "GET", URL: "http://x/socket.io/"}) {
		t.Error("GET is not Socket.IO")
	}
}

// A body message is `event json`, `event text` (sent as a string),
// `event` alone, or a JSON array ["event", args…].
func TestParseSIOEmit(t *testing.T) {
	for _, tc := range []struct{ in, event, args string }{
		{`chat message {"text": "hi"}`, "chat", `"message {\"text\": \"hi\"}"`},
		{`chat {"text": "hi"}`, "chat", `{"text": "hi"}`},
		{`chat hello there`, "chat", `"hello there"`},
		{`ping`, "ping", ``},
		{"note\n42", "note", `42`},
		{`["chat message", {"text": "hi"}, 2]`, "chat message", `{"text": "hi"}|2`},
	} {
		e, err := parseSIOEmit(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		var parts []string
		for _, a := range e.args {
			parts = append(parts, string(a))
		}
		if e.event != tc.event || strings.Join(parts, "|") != tc.args {
			t.Errorf("%q → %q %q", tc.in, e.event, strings.Join(parts, "|"))
		}
	}
	for _, bad := range []string{`[1, 2]`, `[oops`, `[]`} {
		if _, err := parseSIOEmit(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// The plan reads emits, @auth (an object), @listen (capped), @ack,
// @path and the headers.
func TestHTTPSIOPlan(t *testing.T) {
	rr := httpSIOResolved("http://h:3000/chat", "join {\"room\":1}\n===\n[\"typing\", true]", map[string]string{"auth": `{"token":"t"}`, "listen": "2m", "ack": "", "path": "/io"})
	rr.Headers = []httpclient.Header{{Name: "Authorization", Value: "Bearer x"}}
	p, err := httpSIOPlanFor(rr)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.emits) != 2 || p.emits[0].event != "join" || p.emits[1].event != "typing" || string(p.auth) != `{"token":"t"}` || p.listen != httpSIOListenMax || !p.ack || p.header.Get("Authorization") != "Bearer x" {
		t.Fatalf("plan = %+v", p)
	}
	if p.target.Namespace != "/chat" || !strings.Contains(p.target.EngineURL(), "/io/?EIO=4") {
		t.Errorf("target = %+v", p.target)
	}
	p, err = httpSIOPlanFor(httpSIOResolved("http://h", "", nil))
	if err != nil || p.listen != httpSIOListenDefault || len(p.emits) != 0 {
		t.Fatalf("listen-only plan = %+v, %v", p, err)
	}
	for _, dirs := range []map[string]string{{"auth": "[1]"}, {"listen": "soon"}} {
		if _, err := httpSIOPlanFor(httpSIOResolved("http://h", "", dirs)); err == nil {
			t.Errorf("%v: no error", dirs)
		}
	}
	if _, err := httpSIOPlanFor(httpSIOResolved("ftp://h", "", nil)); err == nil {
		t.Error("ftp: no error")
	}
}

// A session against a small Socket.IO server: the page shows the
// connect, the emit with its ack and the received event.
func TestHTTPSIOSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		ctx := context.Background()
		write := func(s string) { _ = ws.Write(ctx, websocket.MessageText, []byte(s)) }
		write(`0{"sid":"e1","pingInterval":25000,"pingTimeout":20000}`)
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			switch msg := string(b); {
			case msg == "40":
				write(`40{"sid":"s1"}`)
			case strings.HasPrefix(msg, `420["join"`):
				write(`430["joined"]`)
				write(`42["news",{"n":1}]`)
			}
		}
	}))
	defer srv.Close()
	p, err := httpSIOPlanFor(httpSIOResolved(srv.URL, `join {"room":1}`, map[string]string{"ack": "", "listen": "1s"}))
	if err != nil {
		t.Fatal(err)
	}
	text, err := httpSIOSession(context.Background(), p, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sid s1", "connected to namespace /", "→ join\n    {\"room\":1}", "ack\n    \"joined\"", "← news\n    {\"n\":1}", "1 event(s) received"} {
		if !strings.Contains(text, want) {
			t.Errorf("page lacks %q:\n%s", want, text)
		}
	}
}
