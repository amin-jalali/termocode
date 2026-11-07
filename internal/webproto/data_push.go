package webproto

import (
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// Push without our cloud (MOB-32, mobile plan "Push without our own
// cloud"): an Android phone with a UnifiedPush distributor of the user's
// own choice (a self-hosted server or an app on the phone) gives the
// gateway its endpoint URL. When a card starts waiting on the laptop and
// the phone has no live link, the gateway POSTs a fixed, content-free
// "wake" to that endpoint; the phone then opens its link and reads the
// cards as usual. The push carries nothing: no count, no title, no id.
//
// push.subscribe / push.unsubscribe are the gateway's own ops (it keeps
// the endpoints, in memory only, for this run); the hub never sees them.
// The gateway tells the hub with web.push_watch (control connection
// only) whether any phone wants wakes; only then does the hub count the
// waiting cards and send a wake event ({"count":N}) on the control
// connection when the count goes up. Web Push (MOB-30) adds its own kind.

// Op names.
const (
	OpPushSubscribe   = "push.subscribe"
	OpPushUnsubscribe = "push.unsubscribe"
	OpWebPushWatch    = "web.push_watch"
)

// PushUnifiedPush is the kind of a UnifiedPush endpoint.
const PushUnifiedPush = "unifiedpush"

// MaxPushEndpoint caps the endpoint URL, in bytes.
const MaxPushEndpoint = 1024

// PushSubscribeArgs gives the gateway this device's push endpoint. A new
// one replaces the device's old one.
type PushSubscribeArgs struct {
	Kind     string `json:"kind" enum:"unifiedpush"`
	Endpoint string `json:"endpoint"`
}

// PushUnsubscribeArgs drops this device's endpoint of Kind.
type PushUnsubscribeArgs struct {
	Kind string `json:"kind" enum:"unifiedpush"`
}

// PushWatchArgs tells the hub whether any phone wants wakes now.
type PushWatchArgs struct {
	On bool `json:"on"`
}

// EvWake is the event kind, on the control connection, of "more cards
// wait now"; its data is a WakeNote.
const EvWake = "wake"

// WakeNote is a wake event's data: how many cards wait. The gateway does
// not put it in the push.
type WakeNote struct {
	Count int `json:"count"`
}

func (a *PushSubscribeArgs) validate(int) error {
	if a.Kind != PushUnifiedPush || !ValidPushEndpoint(a.Endpoint) {
		return invalid("bad push endpoint")
	}
	return nil
}

func (a *PushUnsubscribeArgs) validate(int) error {
	if a.Kind != PushUnifiedPush {
		return invalid("bad push kind")
	}
	return nil
}

// ValidPushEndpoint reports whether s is an endpoint the gateway may
// POST a wake to: https, a host, no user info, no fragment, no control
// character, at most MaxPushEndpoint bytes, and not an address of this
// machine (loopback, unspecified), a link-local one (cloud metadata) or a
// multicast one. Home and tailnet addresses are fine: a self-hosted
// distributor lives there. The gateway checks the address again when it
// dials (a name may resolve to anything).
func ValidPushEndpoint(s string) bool {
	if s == "" || len(s) > MaxPushEndpoint || hasControl(s) || strings.ContainsAny(s, " \\") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.Contains(s, "#") {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if p := u.Port(); p != "" && (len(p) > 5 || p == "0") {
		return false
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return PushAddrOK(ip)
	}
	return true
}

// PushAddrOK reports whether a wake may go to ip: not loopback,
// unspecified, link-local or multicast.
func PushAddrOK(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsMulticast()
}

var opPushRows = []Policy{
	{Op: OpPushSubscribe, New: newOf[PushSubscribeArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 2 * kib, Audit: AuditYes, Phase: PhaseV2, Gateway: true},
	{Op: OpPushUnsubscribe, New: newOf[PushUnsubscribeArgs](), Role: RoleViewer, Rate: Rate{N: 1, Per: 5 * time.Second}, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2, Gateway: true},
	{Op: OpWebPushWatch, New: newOf[PushWatchArgs](), Role: RoleCtl, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPushRows...); return true }()
