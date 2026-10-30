package webproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Msg is one protocol message. Its T field must equal Type().
type Msg interface {
	Type() string
}

// Role is what a browser connection may do.
type Role string

// Roles. In v1 every connection is a controller.
const (
	RoleViewer     Role = "viewer"     // read topics and read ops only (v2)
	RoleController Role = "controller" // may type, edit and answer cards
)

// ErrCode is the fixed set of error codes in an err message.
type ErrCode string

// Error codes.
const (
	CodeBadRequest ErrCode = "bad_request"
	CodeForbidden  ErrCode = "forbidden"
	CodeLimit      ErrCode = "limit"
	CodeConflict   ErrCode = "conflict"
	CodeGone       ErrCode = "gone"
	CodeBusy       ErrCode = "busy"
	CodeNotFound   ErrCode = "not_found"
	CodeInternal   ErrCode = "internal"
)

// ErrCodes lists every error code.
func ErrCodes() []ErrCode {
	return []ErrCode{CodeBadRequest, CodeForbidden, CodeLimit, CodeConflict,
		CodeGone, CodeBusy, CodeNotFound, CodeInternal}
}

// ---- Browser → gateway → hub ----

// Hello is the first message of a connection.
type Hello struct {
	T          string  `json:"t"`
	V          int     `json:"v"`
	Token      string  `json:"token"`
	DeviceName string  `json:"device_name,omitempty"`
	Resume     *Resume `json:"resume,omitempty"`
}

// Resume asks to continue an earlier connection: its id and the last seq
// the browser saw per topic.
type Resume struct {
	Conn   string            `json:"conn"`
	Topics map[string]uint64 `json:"topics"`
}

// Sub subscribes to topics.
type Sub struct {
	T      string   `json:"t"`
	Topics []string `json:"topics"`
}

// Unsub drops topic subscriptions.
type Unsub struct {
	T      string   `json:"t"`
	Topics []string `json:"topics"`
}

// Req is a request. Args is checked against the op's args type.
type Req struct {
	T    string          `json:"t"`
	ID   uint64          `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Ping keeps the connection alive; Here is true while the page is visible.
type Ping struct {
	T    string `json:"t"`
	N    string `json:"n"`
	Here bool   `json:"here,omitempty"`
}

// Bye is the browser saying goodbye.
type Bye struct {
	T string `json:"t"`
}

// ---- Hub → gateway → browser ----

// Meta tells the browser who it is and what it may do.
type Meta struct {
	T       string   `json:"t"`
	V       int      `json:"v"`
	Conn    string   `json:"conn"`
	Role    Role     `json:"role"`
	Device  string   `json:"device"`
	IdleS   int      `json:"idle_s"`
	MaxS    int      `json:"max_s"`
	Viewers int      `json:"viewers"`
	UI      MetaUI   `json:"ui"`
	Caps    []string `json:"caps"`
}

// MetaUI is the laptop's look, so the page can match it.
type MetaUI struct {
	Theme   string            `json:"theme"`
	Palette map[string]string `json:"palette,omitempty"`
	Font    string            `json:"font,omitempty"`
	RTL     bool              `json:"rtl,omitempty"`
}

// Snap is the full state of a topic.
type Snap struct {
	T     string          `json:"t"`
	Topic string          `json:"topic"`
	Seq   uint64          `json:"seq"`
	Data  json.RawMessage `json:"data"`
}

// Delta is one change of a topic; Seq is the previous seq + 1.
type Delta struct {
	T     string          `json:"t"`
	Topic string          `json:"topic"`
	Seq   uint64          `json:"seq"`
	Data  json.RawMessage `json:"data"`
}

// Resync says deltas of a topic were dropped; the browser subscribes again.
type Resync struct {
	T     string `json:"t"`
	Topic string `json:"topic"`
}

// Res is the reply to a request.
type Res struct {
	T    string          `json:"t"`
	ID   uint64          `json:"id"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ErrMsg is an error, for a request (ID set) or for the connection.
// Msg is short English for the user; never a path or a secret.
type ErrMsg struct {
	T    string          `json:"t"`
	ID   uint64          `json:"id,omitempty"`
	Code ErrCode         `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Event is a one-shot event not tied to a topic.
type Event struct {
	T    string          `json:"t"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Pong answers a ping with the same N.
type Pong struct {
	T string `json:"t"`
	N string `json:"n"`
}

// ServerBye is sent right before the gateway closes with Code.
type ServerBye struct {
	T    string    `json:"t"`
	Code CloseCode `json:"code"`
}

// Message kinds (the "t" field).
const (
	KindHello  = "hello"
	KindSub    = "sub"
	KindUnsub  = "unsub"
	KindReq    = "req"
	KindPing   = "ping"
	KindBye    = "bye"
	KindMeta   = "meta"
	KindSnap   = "snap"
	KindDelta  = "delta"
	KindResync = "resync"
	KindRes    = "res"
	KindErr    = "err"
	KindEv     = "ev"
	KindPong   = "pong"
)

// Type methods.
func (*Hello) Type() string     { return KindHello }
func (*Sub) Type() string       { return KindSub }
func (*Unsub) Type() string     { return KindUnsub }
func (*Req) Type() string       { return KindReq }
func (*Ping) Type() string      { return KindPing }
func (*Bye) Type() string       { return KindBye }
func (*Meta) Type() string      { return KindMeta }
func (*Snap) Type() string      { return KindSnap }
func (*Delta) Type() string     { return KindDelta }
func (*Resync) Type() string    { return KindResync }
func (*Res) Type() string       { return KindRes }
func (*ErrMsg) Type() string    { return KindErr }
func (*Event) Type() string     { return KindEv }
func (*Pong) Type() string      { return KindPong }
func (*ServerBye) Type() string { return KindBye }

var clientKinds = map[string]func() Msg{
	KindHello: func() Msg { return &Hello{} },
	KindSub:   func() Msg { return &Sub{} },
	KindUnsub: func() Msg { return &Unsub{} },
	KindReq:   func() Msg { return &Req{} },
	KindPing:  func() Msg { return &Ping{} },
	KindBye:   func() Msg { return &Bye{} },
}

var serverKinds = map[string]func() Msg{
	KindMeta:   func() Msg { return &Meta{} },
	KindSnap:   func() Msg { return &Snap{} },
	KindDelta:  func() Msg { return &Delta{} },
	KindResync: func() Msg { return &Resync{} },
	KindRes:    func() Msg { return &Res{} },
	KindErr:    func() Msg { return &ErrMsg{} },
	KindEv:     func() Msg { return &Event{} },
	KindPong:   func() Msg { return &Pong{} },
	KindBye:    func() Msg { return &ServerBye{} },
}

// ClientKinds lists the kinds a browser may send, sorted.
func ClientKinds() []string { return sortedKeys(clientKinds) }

// ServerKinds lists the kinds the hub sends to a browser, sorted.
func ServerKinds() []string { return sortedKeys(serverKinds) }

func sortedKeys(m map[string]func() Msg) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ErrUnknownKind is returned for a "t" this direction does not know.
var ErrUnknownKind = errors.New("webproto: unknown message type")

// Encode marshals a message. The T field must already equal Type(), so a
// message can never go out under the wrong type.
func Encode(m Msg) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var head struct {
		T string `json:"t"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, err
	}
	if head.T != m.Type() {
		return nil, fmt.Errorf("webproto: encode %T: t is %q, want %q", m, head.T, m.Type())
	}
	return b, nil
}

// peekKind reads only the "t" field.
func peekKind(b []byte) (string, error) {
	var head struct {
		T string `json:"t"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return "", err
	}
	return head.T, nil
}

// DecodeServer decodes a hub → browser message. It is tolerant: unknown
// fields are ignored, so the hub may add optional fields later.
func DecodeServer(b []byte) (Msg, error) {
	kind, err := peekKind(b)
	if err != nil {
		return nil, err
	}
	mk, ok := serverKinds[kind]
	if !ok {
		return nil, ErrUnknownKind
	}
	m := mk()
	if err := json.Unmarshal(b, m); err != nil {
		return nil, err
	}
	return m, nil
}
