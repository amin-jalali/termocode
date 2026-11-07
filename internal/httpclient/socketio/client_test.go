package socketio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeServer is a tiny Engine.IO v4 / Socket.IO server on one namespace.
// It records the text frames it reads on got.
func fakeServer(t *testing.T, ns string, refuse bool) (*httptest.Server, chan string) {
	t.Helper()
	got := make(chan string, 32)
	prefix := ""
	if ns != "/" {
		prefix = ns + ","
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket.io/" || r.URL.Query().Get("EIO") != "4" || r.URL.Query().Get("transport") != "websocket" {
			http.Error(w, "bad handshake", http.StatusBadRequest)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		ctx := context.Background()
		write := func(s string) { _ = ws.Write(ctx, websocket.MessageText, []byte(s)) }
		write(`0{"sid":"e1","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1000000}`)
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			msg := string(b)
			got <- msg
			switch {
			case strings.HasPrefix(msg, "40"):
				if refuse {
					write(`44` + prefix + `{"message":"not authorized"}`)
					continue
				}
				write(`40` + prefix + `{"sid":"s1"}`)
				write(`2`) // a ping
				write(`42` + prefix + `["welcome",{"n":1}]`)
				write(`42` + prefix + `5["question"]`)
			case strings.HasPrefix(msg, "42"+prefix):
				body := strings.TrimPrefix(msg, "42"+prefix)
				if i := strings.IndexByte(body, '['); i > 0 {
					write(`43` + prefix + body[:i] + `["ok"]`)
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func expectFrame(t *testing.T, got chan string, want string) {
	t.Helper()
	select {
	case g := <-got:
		if g != want {
			t.Fatalf("server read %q, want %q", g, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("server: timeout waiting for %q", want)
	}
}

// Dial joins the namespace with the auth payload, answers the ping and
// the server's ack request, delivers events and gets an ack back.
func TestClientSession(t *testing.T) {
	srv, got := fakeServer(t, "/chat", false)
	tg, err := ParseURL(srv.URL+"/chat", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, tg, Options{Auth: json.RawMessage(`{"token":"t"}`)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	expectFrame(t, got, `40/chat,{"token":"t"}`)
	if c.SID() != "s1" {
		t.Errorf("sid = %q", c.SID())
	}
	expectFrame(t, got, `3`)
	ev := <-c.Events()
	if ev.Name != "welcome" || len(ev.Args) != 1 || string(ev.Args[0]) != `{"n":1}` {
		t.Fatalf("event = %+v", ev)
	}
	ev = <-c.Events()
	if ev.Name != "question" {
		t.Fatalf("event = %+v", ev)
	}
	expectFrame(t, got, `43/chat,5[]`)
	if err := c.Emit(ctx, "typing", []json.RawMessage{json.RawMessage(`true`)}); err != nil {
		t.Fatal(err)
	}
	expectFrame(t, got, `42/chat,["typing",true]`)
	ack, err := c.EmitAck(ctx, "chat", []json.RawMessage{json.RawMessage(`"hi"`)})
	if err != nil {
		t.Fatal(err)
	}
	expectFrame(t, got, `42/chat,0["chat","hi"]`)
	if len(ack) != 1 || string(ack[0]) != `"ok"` {
		t.Errorf("ack = %s", ack)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	expectFrame(t, got, `41/chat,`)
	if _, ok := <-c.Events(); ok {
		t.Error("Events still open after Close")
	}
}

// A CONNECT_ERROR is a dial error with the server's message.
func TestClientRefused(t *testing.T) {
	srv, _ := fakeServer(t, "/", true)
	tg, _ := ParseURL(srv.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Dial(ctx, tg, Options{})
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Dial(ctx, tg, Options{Auth: json.RawMessage(`[1]`)}); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("array auth: err = %v", err)
	}
}
