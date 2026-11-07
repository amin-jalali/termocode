package socketio

import (
	"encoding/json"
	"strings"
	"testing"
)

// Packets read namespace, ack id and data; encode is the reverse.
func TestDecodeEncodePacket(t *testing.T) {
	for _, tc := range []struct {
		in       string
		typ, id  int
		ns, data string
	}{
		{"0", pConnect, -1, "/", ""},
		{`0/chat,{"sid":"x"}`, pConnect, -1, "/chat", `{"sid":"x"}`},
		{`2["hi",1]`, pEvent, -1, "/", `["hi",1]`},
		{`2/chat,12["hi"]`, pEvent, 12, "/chat", `["hi"]`},
		{`37["ok"]`, pAck, 7, "/", `["ok"]`},
		{`4{"message":"no"}`, pConnectError, -1, "/", `{"message":"no"}`},
		{`1/chat`, pDisconnect, -1, "/chat", ""},
	} {
		p, err := decodePacket(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if p.typ != tc.typ || p.id != tc.id || p.ns != tc.ns || string(p.data) != tc.data {
			t.Errorf("%q → %+v", tc.in, p)
		}
		if tc.in != `1/chat` && p.encode() != tc.in {
			t.Errorf("%q encodes as %q", tc.in, p.encode())
		}
	}
	p, err := decodePacket(`51-["file",{"_placeholder":true,"num":0}]`)
	if err != nil || p.typ != pBinaryEvent || !strings.HasPrefix(string(p.data), `["file"`) {
		t.Errorf("binary event = %+v, %v", p, err)
	}
	for _, bad := range []string{"", "9", "2[oops", "5[]"} {
		if _, err := decodePacket(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestEventData(t *testing.T) {
	d, err := eventData("chat message", []json.RawMessage{json.RawMessage(`{"text":"hi"}`), json.RawMessage(`2`)})
	if err != nil || string(d) != `["chat message",{"text":"hi"},2]` {
		t.Fatalf("data = %s, %v", d, err)
	}
	if _, err := eventData("x", []json.RawMessage{json.RawMessage(`{`)}); err == nil {
		t.Error("bad argument: no error")
	}
	name, args, err := splitEvent(d)
	if err != nil || name != "chat message" || len(args) != 2 {
		t.Errorf("split = %q %v %v", name, args, err)
	}
	if _, _, err := splitEvent(json.RawMessage(`[1]`)); err == nil {
		t.Error("number name: no error")
	}
}

// The URL path is the namespace; the engine URL is the Socket.IO path
// with EIO=4 and the websocket transport plus the user's query.
func TestParseURL(t *testing.T) {
	tg, err := ParseURL("https://ann:pw@chat.local:8443/admin/?token=x", "")
	if err != nil {
		t.Fatal(err)
	}
	if !tg.TLS || tg.Namespace != "/admin" || tg.String() != "https://chat.local:8443/admin" {
		t.Fatalf("target = %+v", tg)
	}
	if got := tg.EngineURL(); got != "wss://chat.local:8443/socket.io/?EIO=4&token=x&transport=websocket" {
		t.Errorf("engine = %q", got)
	}
	tg, err = ParseURL("ws://b:3000", "my-io")
	if err != nil || tg.Namespace != "/" || tg.EngineURL() != "ws://b:3000/my-io/?EIO=4&transport=websocket" || tg.String() != "http://b:3000" {
		t.Fatalf("target = %+v, %v", tg, err)
	}
	for _, bad := range []string{"mqtt://b", "http://"} {
		if _, err := ParseURL(bad, ""); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
