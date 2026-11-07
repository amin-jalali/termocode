package webproto

import (
	"encoding/base64"

	"github.com/amin-jalali/termocode/internal/webpush"
)

// Web Push (MOB-30): a paired phone asks the laptop to wake it when
// something waits for the user while the page is closed. push.key gives
// the laptop's VAPID public key (make: create it on first use) and
// whether this device gets pushes; push.subscribe stores the browser's
// subscription for this device, push.unsubscribe forgets it. Both are
// kept in the laptop's secret store, never in a file of the web panel.
// A push says only a count (web_push_detail), never content.

// Push op names.
const (
	OpPushKey         = "push.key"
	OpPushSubscribe   = "push.subscribe"
	OpPushUnsubscribe = "push.unsubscribe"
)

// PushKeyArgs: Make creates the key when there is none yet.
type PushKeyArgs struct {
	Make bool `json:"make,omitempty"`
}

// PushSubArgs is the browser's PushSubscription.toJSON(): its endpoint
// and keys (base64url).
type PushSubArgs struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

func (a *PushSubArgs) validate(int) error {
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
	{Op: OpPushUnsubscribe, New: newOf[EmptyArgs](), Role: RoleViewer, Rate: slow, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPushRows...); return true }()

func init() {
	registerResult(OpPushKey, PushState{})
	registerResult(OpPushSubscribe, PushState{})
	registerResult(OpPushUnsubscribe, PushState{})
}
