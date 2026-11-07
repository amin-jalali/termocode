package mqtt

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// ws:// and wss:// are MQTT over WebSocket: the path is the endpoint
// (default /mqtt), the #fragment is the topic, the query stays on the
// endpoint but not in String.
func TestParseURLWebSocket(t *testing.T) {
	tg, err := ParseURL("wss://ann:pw@broker.local/ws?token=x#sensors/kitchen")
	if err != nil {
		t.Fatal(err)
	}
	if !tg.WS || !tg.TLS || tg.Addr != "broker.local:443" || tg.Path != "/ws" || tg.RawQuery != "token=x" || tg.Topic != "sensors/kitchen" || tg.Username != "ann" || tg.Password != "pw" {
		t.Fatalf("target = %+v", tg)
	}
	if got := tg.String(); got != "wss://broker.local:443/ws" {
		t.Errorf("String = %q", got)
	}
	if got := tg.wsURL(); got != "wss://broker.local:443/ws?token=x" {
		t.Errorf("wsURL = %q", got)
	}
	tg, err = ParseURL("ws://b:9001")
	if err != nil || tg.TLS || tg.Path != "/mqtt" || tg.Addr != "b:9001" || tg.Topic != "" {
		t.Fatalf("target = %+v, %v", tg, err)
	}
	tg, err = ParseURL("mqtt://b/a/b")
	if err != nil || tg.WS || tg.Topic != "a/b" {
		t.Fatalf("mqtt target = %+v, %v", tg, err)
	}
	if _, err := ParseURL("http://b"); err == nil {
		t.Error("http:// is not MQTT")
	}
}

// DialTarget on a ws:// URL asks for the mqtt subprotocol and runs the
// MQTT handshake and a publish over binary WebSocket frames.
func TestDialTargetWebSocket(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- "path " + r.URL.Path
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"mqtt"}})
		if err != nil {
			return
		}
		got <- "proto " + ws.Subprotocol()
		conn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		defer func() { _ = conn.Close() }()
		br := bufio.NewReader(conn)
		if h, _, err := readPacket(br); err != nil || h != 0x10 {
			got <- "bad connect"
			return
		}
		_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x00})
		h, body, err := readPacket(br)
		if err != nil || h&0xF0 != 0x30 {
			got <- "bad publish"
			return
		}
		m, _, _ := parsePublish(h, body)
		got <- "publish " + m.Topic + " " + string(m.Payload)
	}))
	defer srv.Close()
	tg, err := ParseURL("ws://" + strings.TrimPrefix(srv.URL, "http://") + "#room/1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialTarget(ctx, tg, Options{ClientID: "tc", CleanSession: true, KeepAlive: 30 * time.Second}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Publish(tg.Topic, []byte("on"), false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"path /mqtt", "proto mqtt", "publish room/1 on"} {
		select {
		case g := <-got:
			if g != want {
				t.Fatalf("server: %q, want %q", g, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("server: timeout waiting for %q", want)
		}
	}
}
