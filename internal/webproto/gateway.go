package webproto

// The gateway talks to the hub over a unix socket, one connection per
// browser plus one control connection. The first line of each connection
// is one of the messages below; after it, the browser's messages pass
// through unchanged. The hub trusts these lines only because the socket
// peer is the same user (ADR 0020).

// Open starts a browser connection on the hub.
type Open struct {
	T      string `json:"t"`
	Conn   string `json:"conn"`
	Role   Role   `json:"role"`
	Device string `json:"device"`
	Name   string `json:"name,omitempty"`
}

// Ctl starts the gateway's control connection.
type Ctl struct {
	T string `json:"t"`
}

// Gateway line kinds.
const (
	KindOpen = "open"
	KindCtl  = "ctl"
)

// Type methods.
func (*Open) Type() string { return KindOpen }
func (*Ctl) Type() string  { return KindCtl }

var gatewayKinds = map[string]func() Msg{
	KindOpen: func() Msg { return &Open{} },
	KindCtl:  func() Msg { return &Ctl{} },
}

// GatewayKinds lists the first-line kinds of a hub connection, sorted.
func GatewayKinds() []string { return sortedKeys(gatewayKinds) }

// DecodeGatewayLine decodes the first line of a hub connection strictly.
func DecodeGatewayLine(b []byte) (Msg, error) {
	return decodeStrict(b, gatewayKinds)
}

// ValidRole reports whether r is a known role.
func ValidRole(r Role) bool { return r == RoleViewer || r == RoleController }

func (o *Open) validate(int) error {
	if o.Conn == "" || hasControl(o.Conn) || o.Device == "" || hasControl(o.Device) {
		return invalid("open without a connection or device id")
	}
	if !ValidRole(o.Role) {
		return invalid("role %q", o.Role)
	}
	if len(o.Name) > MaxDeviceName || hasControl(o.Name) {
		return invalid("bad device name")
	}
	return nil
}

// EvAudit is the event kind, on the control connection, of a summary of
// one web op for the gateway's audit log (web design 3.5).
const EvAudit = "audit"

// AuditNote is what the hub tells the gateway's audit log about one web
// op: ids, the op, the result and a short summary built from the policy
// table (a path, a label, a count: never chat, file or terminal text, or
// a secret).
type AuditNote struct {
	Conn    string `json:"conn,omitempty"`
	Device  string `json:"device"`
	Op      string `json:"op"`
	OK      bool   `json:"ok"`
	Why     string `json:"why,omitempty"` // a fixed name: an err code, "laptop_card", "confirmed_laptop", …
	Summary string `json:"summary,omitempty"`
}
