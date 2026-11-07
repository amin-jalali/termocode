package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// echoWS is a WebSocket server that sends back every text message,
// prefixed with the X-Token header it got.
func echoWS(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		tok := r.Header.Get("X-Token")
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if err := c.Write(r.Context(), typ, []byte(tok+":"+string(b))); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(srv *httptest.Server) string { return "ws" + strings.TrimPrefix(srv.URL, "http") }

func TestWebSocketSendReceive(t *testing.T) {
	srv := echoWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := New(Options{})
	rr := Resolved{Request: Request{Method: "WEBSOCKET", URL: wsURL(srv), Headers: []Header{{Name: "X-Token", Value: "abc"}}}}
	conn, err := c.DialWS(ctx, rr, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Send(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	m, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m.Text != "abc:hello" || m.Dir != WSIn || m.Binary {
		t.Fatalf("got %+v", m)
	}
	log := conn.Log()
	if len(log) != 2 || log[0].Dir != WSOut || log[0].Text != "hello" || log[1].Text != "abc:hello" {
		t.Fatalf("log %+v", log)
	}
}

// The SSRF guard (AI sends) also covers WebSocket connects.
func TestWebSocketBlockedByAllowIP(t *testing.T) {
	srv := echoWS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := New(Options{})
	_, err := c.DialWS(ctx, Resolved{Request: Request{Method: "WEBSOCKET", URL: wsURL(srv)}},
		Hooks{AllowIP: func(ip net.IP) bool { return !ip.IsLoopback() }})
	if err == nil || !strings.Contains(err.Error(), "blocked: private address") {
		t.Fatalf("err = %v, want blocked", err)
	}
}

// Only ws:// and wss:// (or http(s), mapped) URLs are dialed.
func TestWebSocketBadScheme(t *testing.T) {
	c := New(Options{})
	_, err := c.DialWS(context.Background(), Resolved{Request: Request{Method: "WEBSOCKET", URL: "ftp://x"}}, Hooks{})
	if err == nil {
		t.Fatal("ftp:// dialed")
	}
}

// The log keeps the newest messages only.
func TestWebSocketLogCap(t *testing.T) {
	l := &wsLog{max: 3}
	for _, s := range []string{"a", "b", "c", "d"} {
		l.add(WSMessage{Dir: WSOut, Text: s})
	}
	got := l.all()
	if len(got) != 3 || got[0].Text != "b" || got[2].Text != "d" {
		t.Fatalf("%+v", got)
	}
}

func TestIsWebSocket(t *testing.T) {
	for _, r := range []Request{{Method: "WEBSOCKET", URL: "{{host}}/ws"}, {Method: "GET", URL: "ws://h/x"}, {Method: "GET", URL: "WSS://h"}} {
		if !IsWebSocket(r) {
			t.Errorf("%+v not a WebSocket request", r)
		}
	}
	if IsWebSocket(Request{Method: "GET", URL: "https://h"}) {
		t.Error("https is not WebSocket")
	}
}

// Saved messages: the body holds messages split by `===` lines.
func TestWSSavedMessages(t *testing.T) {
	body := "{\"op\":\"sub\"}\n===\nping\n\n===\n\n===\nmulti\nline\n"
	want := []string{`{"op":"sub"}`, "ping", "multi\nline"}
	if got := WSMessages(&body); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	if WSMessages(nil) != nil {
		t.Fatal("nil body")
	}
}

// After a parse round trip a WEBSOCKET request keeps its method, URL,
// headers and saved messages.
func TestWebSocketParse(t *testing.T) {
	f := Parse("WEBSOCKET ws://localhost:8080/ws\nX-Token: t\n\nhello\n===\nbye\n")
	if len(f.Requests) != 1 {
		t.Fatalf("%+v", f)
	}
	r := f.Requests[0]
	if r.Method != "WEBSOCKET" || r.URL != "ws://localhost:8080/ws" || r.Header("X-Token") != "t" {
		t.Fatalf("%+v", r)
	}
	if got := WSMessages(r.Body); !reflect.DeepEqual(got, []string{"hello", "bye"}) {
		t.Fatalf("%q", got)
	}
}
