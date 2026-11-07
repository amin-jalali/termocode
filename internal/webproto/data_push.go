package webproto

import (
	"encoding/json"
	"net/netip"
	"net/url"
	"strings"
)

// Push without our cloud (MOB-32, mobile plan "Push without our own
// cloud"): an Android phone with a UnifiedPush distributor of the user's
// own choice (a self-hosted server or an app on the phone) gives the
// gateway its endpoint URL. When a card starts waiting on the laptop and
// the phone has no live link, the gateway POSTs a fixed, content-free
// "wake" to that endpoint; the phone then opens its link and reads the
// cards as usual. The push carries nothing: no count, no title, no id.
//
// push.subscribe / push.unsubscribe (ops_push.go) are one op pair for
// both kinds: kind "unifiedpush" is answered by the gateway itself (it
// keeps the endpoints, in memory only, for this run; the hub never sees
// it), kind "webpush" (or none) goes to the hub (MOB-30).
// The gateway tells the hub with web.push_watch (control connection
// only) whether any phone wants wakes; only then does the hub count the
// waiting cards and send a wake event ({"count":N}) on the control
// connection when the count goes up. Web Push (MOB-30) adds its own kind.

// OpWebPushWatch: the gateway tells the hub whether any phone wants wakes.
const OpWebPushWatch = "web.push_watch"

// Push kinds (PushSubArgs.Kind, PushUnsubscribeArgs.Kind).
const (
	PushWebPush     = "webpush"     // a browser's Web Push subscription (MOB-30, hub); also the empty kind
	PushUnifiedPush = "unifiedpush" // a UnifiedPush endpoint (MOB-32, gateway)
)

// PushKindOf reads the kind of push.subscribe / push.unsubscribe args
// without a full decode, so the gateway can route them; a missing or
// bad kind is webpush (the hub refuses what does not decode).
func PushKindOf(raw json.RawMessage) string {
	var k struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &k) != nil || k.Kind != PushUnifiedPush {
		return PushWebPush
	}
	return PushUnifiedPush
}

// MaxPushEndpoint caps the endpoint URL, in bytes.
const MaxPushEndpoint = 1024

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

var opPushWatchRows = []Policy{
	{Op: OpWebPushWatch, New: newOf[PushWatchArgs](), Role: RoleCtl, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPushWatchRows...); return true }()
