package webproto

import (
	"encoding/base64"
	"strings"

	"github.com/amin-jalali/termocode/internal/webpush"
)

// Web Push (MOB-30): a paired phone asks the laptop to wake it when
// something waits for the user while the page is closed. push.key gives
// the laptop's VAPID public key (make: create it on first use) and
// whether this device gets pushes; push.subscribe stores the browser's
// subscription for this device, push.unsubscribe forgets it. Both are
// kept in the laptop's secret store, never in a file of the web panel.
// A push says only a count (web_push_detail), never content.
// The same two ops with kind "unifiedpush" are the gateway's (MOB-32,
// data_push.go): it answers them itself and never forwards them.

// Push op names.
const (
	OpPushKey         = "push.key"
	OpPushSubscribe   = "push.subscribe"
	OpPushUnsubscribe = "push.unsubscribe"
	OpPushList        = "push.list"   // the web settings' list of subscriptions (CMP-47)
	OpPushRemove      = "push.remove" // remove one device's subscription
)

// PushKeyArgs: Make creates the key when there is none yet.
type PushKeyArgs struct {
	Make bool `json:"make,omitempty"`
}

// PushSubArgs is one push subscription of this device. Kind webpush (or
// none, MOB-30): the browser's PushSubscription.toJSON(), its endpoint
// and keys (base64url), kept by the hub. Kind unifiedpush (MOB-32): a
// UnifiedPush endpoint only, kept by the gateway (data_push.go). A new
// one replaces the device's old one of the same kind.
type PushSubArgs struct {
	Kind     string `json:"kind,omitempty" enum:"webpush|unifiedpush"`
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh,omitempty"`
	Auth     string `json:"auth,omitempty"`
}

// PushUnsubscribeArgs drops this device's subscription of Kind (none:
// webpush).
type PushUnsubscribeArgs struct {
	Kind string `json:"kind,omitempty" enum:"webpush|unifiedpush"`
}

func (a *PushUnsubscribeArgs) validate(int) error {
	if a.Kind != "" && a.Kind != PushWebPush && a.Kind != PushUnifiedPush {
		return invalid("bad push kind")
	}
	return nil
}

func (a *PushSubArgs) validate(int) error {
	switch a.Kind {
	case PushUnifiedPush:
		if !ValidPushEndpoint(a.Endpoint) || a.P256dh != "" || a.Auth != "" {
			return invalid("bad push endpoint")
		}
		return nil
	case "", PushWebPush:
	default:
		return invalid("bad push kind")
	}
	if webpush.CheckEndpoint(a.Endpoint, nil) != nil {
		return invalid("not a push service")
	}
	b64 := base64.RawURLEncoding
	if k, err := b64.DecodeString(a.P256dh); err != nil || len(k) != 65 {
		return invalid("bad p256dh")
	}
	if k, err := b64.DecodeString(a.Auth); err != nil || len(k) != 16 {
		return invalid("bad auth")
	}
	return nil
}

// PushListArgs: no arguments.
type PushListArgs struct{}

// PushRemoveArgs names the device whose subscription is removed.
type PushRemoveArgs struct {
	Device string `json:"device"`
}

func (a *PushRemoveArgs) validate(int) error {
	if !strings.HasPrefix(a.Device, "dev-") || len(a.Device) > 40 {
		return invalid("bad device")
	}
	for _, r := range a.Device[4:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return invalid("bad device")
		}
	}
	return nil
}

// PushItem is one device that gets pushes: its name, when it was added
// (ms since 1970) and whether it is the asking device. Never an address.
type PushItem struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Added  int64  `json:"added"`
	This   bool   `json:"this,omitempty"`
}

// PushList is the result of push.list and push.remove.
type PushList struct {
	Items []PushItem `json:"items"`
}

// PushState is the result of the push ops. State: ready (Key is set),
// none (no key yet and make was false), locked (the laptop's secret
// store is locked), off (this connection cannot get pushes: not a
// paired device, or a shared view).
type PushState struct {
	State string `json:"state" enum:"ready|none|locked|off"`
	Key   string `json:"key,omitempty"`
	On    bool   `json:"on"` // this device has a subscription on the laptop
}

var opPushRows = []Policy{
	{Op: OpPushKey, New: newOf[PushKeyArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpPushSubscribe, New: newOf[PushSubArgs](), Role: RoleViewer, Rate: slow, MaxBytes: 2 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpPushList, New: newOf[PushListArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpPushRemove, New: newOf[PushRemoveArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpPushUnsubscribe, New: newOf[PushUnsubscribeArgs](), Role: RoleViewer, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPushRows...); return true }()

func init() {
	registerResult(OpPushKey, PushState{})
	registerResult(OpPushSubscribe, PushState{})
	registerResult(OpPushUnsubscribe, PushState{})
	registerResult(OpPushList, PushList{})
	registerResult(OpPushRemove, PushList{})
}
