// Package socketio is a small Socket.IO client (protocol v5 over
// Engine.IO v4, the WebSocket transport only) for .http SOCKETIO
// requests (API-20). It connects to one namespace, emits events (with
// an optional ack), answers the server's pings and acks, and hands the
// received events to the caller. Binary attachments are not decoded.
package socketio

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Socket.IO packet types.
const (
	pConnect      = 0
	pDisconnect   = 1
	pEvent        = 2
	pAck          = 3
	pConnectError = 4
	pBinaryEvent  = 5
	pBinaryAck    = 6
)

// packet is one decoded Socket.IO packet.
type packet struct {
	typ  int
	ns   string // "/" = the main namespace
	id   int    // ack id, -1 = none
	data json.RawMessage
}

// decodePacket reads <type>[<n>-][<ns>,][<id>][<json>].
func decodePacket(s string) (packet, error) {
	if s == "" || s[0] < '0' || s[0] > '6' {
		return packet{}, fmt.Errorf("socket.io: bad packet %q", clip(s))
	}
	p := packet{typ: int(s[0] - '0'), ns: "/", id: -1}
	s = s[1:]
	if p.typ == pBinaryEvent || p.typ == pBinaryAck {
		i := strings.IndexByte(s, '-')
		if i < 0 {
			return packet{}, errors.New("socket.io: binary packet without an attachment count")
		}
		s = s[i+1:]
	}
	if strings.HasPrefix(s, "/") {
		if i := strings.IndexByte(s, ','); i >= 0 {
			p.ns, s = s[:i], s[i+1:]
		} else {
			p.ns, s = s, ""
		}
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 {
		id, err := strconv.Atoi(s[:i])
		if err != nil {
			return packet{}, fmt.Errorf("socket.io: bad ack id: %v", err)
		}
		p.id, s = id, s[i:]
	}
	if s != "" {
		if !json.Valid([]byte(s)) {
			return packet{}, fmt.Errorf("socket.io: packet data is not JSON: %q", clip(s))
		}
		p.data = json.RawMessage(s)
	}
	return p, nil
}

// encode writes p without the Engine.IO "4" prefix.
func (p packet) encode() string {
	var b strings.Builder
	b.WriteByte(byte('0' + p.typ))
	if p.ns != "" && p.ns != "/" {
		b.WriteString(p.ns)
		b.WriteByte(',')
	}
	if p.id >= 0 {
		b.WriteString(strconv.Itoa(p.id))
	}
	b.Write(p.data)
	return b.String()
}

// eventData is the JSON array [name, args...].
func eventData(name string, args []json.RawMessage) (json.RawMessage, error) {
	for i, a := range args {
		if !json.Valid(a) {
			return nil, fmt.Errorf("socket.io: argument %d of %q is not JSON", i+1, name)
		}
	}
	return json.Marshal(append([]json.RawMessage{mustQuote(name)}, args...))
}

func mustQuote(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// splitEvent reads [name, args...].
func splitEvent(data json.RawMessage) (string, []json.RawMessage, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) == 0 {
		return "", nil, errors.New("socket.io: an event is not a JSON array")
	}
	var name string
	if err := json.Unmarshal(arr[0], &name); err != nil {
		return "", nil, errors.New("socket.io: an event name is not a string")
	}
	return name, arr[1:], nil
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// Target is a Socket.IO server read from a URL.
type Target struct {
	Host      string
	TLS       bool
	Namespace string // from the URL path; "/" = the main one
	engine    string // the Engine.IO WebSocket URL
	shown     string
}

// DefaultPath is where Socket.IO servers listen by default.
const DefaultPath = "/socket.io/"

// ParseURL reads http(s)://host[:port][/namespace][?query] (ws:// and
// wss:// work too). path is the server's Socket.IO path ("" = the
// default /socket.io/). The query is sent with the handshake.
func ParseURL(raw, path string) (Target, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Target{}, fmt.Errorf("socket.io: bad URL: %v", err)
	}
	var t Target
	scheme := "ws"
	switch strings.ToLower(u.Scheme) {
	case "http", "ws":
	case "https", "wss":
		t.TLS, scheme = true, "wss"
	default:
		return Target{}, errors.New("socket.io: the URL must start with http://, https://, ws:// or wss://")
	}
	if u.Hostname() == "" {
		return Target{}, errors.New("socket.io: the URL has no host")
	}
	t.Host = u.Hostname()
	t.Namespace = strings.TrimRight(u.Path, "/")
	if t.Namespace == "" {
		t.Namespace = "/"
	}
	if path = strings.TrimSpace(path); path == "" {
		path = DefaultPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	q := u.Query()
	q.Set("EIO", "4")
	q.Set("transport", "websocket")
	t.engine = (&url.URL{Scheme: scheme, Host: u.Host, Path: path, RawQuery: q.Encode()}).String()
	shownScheme := "http"
	if t.TLS {
		shownScheme = "https"
	}
	t.shown = shownScheme + "://" + u.Host
	if t.Namespace != "/" {
		t.shown += t.Namespace
	}
	return t, nil
}

// String is the server and namespace without user info or query.
func (t Target) String() string { return t.shown }

// EngineURL is the WebSocket URL the client dials.
func (t Target) EngineURL() string { return t.engine }
