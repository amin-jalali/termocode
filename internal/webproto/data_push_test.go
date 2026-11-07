package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOB-32): a paired phone gives the gateway its UnifiedPush
// endpoint. push.subscribe / push.unsubscribe are one op pair for Web
// Push (MOB-30, the hub) and UnifiedPush (MOB-32): the gateway routes
// kind unifiedpush to itself by PushKindOf, so the rows are not Gateway
// rows. A viewer may call them, slowly, and every call is audited. The gateway
// tells the hub with a control-only op when wakes are wanted. push.key
// (MOB-30) is a read: no audit line, but no bigger than the others.
// One limit for all three: at most 2 KiB (a 1 KiB endpoint and its keys).
func TestPushOpRows(t *testing.T) {
	for op, audit := range map[string]Audit{OpPushKey: AuditNo, OpPushSubscribe: AuditYes, OpPushUnsubscribe: AuditYes} {
		p, ok := Lookup(op)
		if !ok || p.Gateway || p.Role != RoleViewer || p.Audit != audit || p.LaptopCard {
			t.Errorf("%s: row %+v", op, p)
		}
		if p.MaxBytes > 2*kib {
			t.Errorf("%s: too big: %+v", op, p)
		}
		if op != OpPushKey && p.Rate.Per/1e9 < 1 {
			t.Errorf("%s: too fast: %+v", op, p)
		}
	}
	if PushKindOf(json.RawMessage(`{"kind":"unifiedpush"}`)) != PushUnifiedPush ||
		PushKindOf(json.RawMessage(`{"endpoint":"x"}`)) != PushWebPush || PushKindOf(nil) != PushWebPush {
		t.Error("PushKindOf routes wrong")
	}
	p, ok := Lookup(OpWebPushWatch)
	if !ok || p.Role != RoleCtl || p.Gateway || p.Audit != AuditYes {
		t.Errorf("web.push_watch: row %+v", p)
	}
	if Allowed(RoleController, OpWebPushWatch) || Allowed(RoleViewer, OpWebPushWatch) {
		t.Error("a browser may call web.push_watch")
	}
}

// Spec (MOB-32): only an https endpoint on a real host is taken: no
// plain http, no user:password, no fragment, no loopback or link-local
// address, no control character, nothing over MaxPushEndpoint bytes;
// a webpush subscription needs its keys, a unifiedpush one has none.
func TestPushSubArgs(t *testing.T) {
	good := []string{
		"https://push.example.org/up/AbC123",
		"https://ntfy.my-tailnet.ts.net/upAbC?up=1",
		"https://192.168.1.20:8443/up/x",
		"https://100.64.0.7/up/x",
	}
	for _, ep := range good {
		raw, _ := json.Marshal(PushSubArgs{Kind: PushUnifiedPush, Endpoint: ep})
		if _, err := DecodeArgs(OpPushSubscribe, raw); err != nil {
			t.Errorf("%s refused: %v", ep, err)
		}
	}
	bad := []string{
		"",
		"http://push.example.org/up/x",
		"ftp://push.example.org/x",
		"https://user:pw@push.example.org/x",
		"https://push.example.org/x#frag",
		"https:///x",
		"https://127.0.0.1/x",
		"https://localhost/x",
		"https://[::1]/x",
		"https://169.254.169.254/latest",
		"https://0.0.0.0/x",
		"https://[fe80::1]/x",
		"https://push.example.org/x\n",
		"https://push.example.org/" + strings.Repeat("a", MaxPushEndpoint),
		"push.example.org/x",
	}
	for _, ep := range bad {
		raw, _ := json.Marshal(PushSubArgs{Kind: PushUnifiedPush, Endpoint: ep})
		if _, err := DecodeArgs(OpPushSubscribe, raw); err == nil {
			t.Errorf("%q taken", ep)
		}
	}
	for _, raw := range []string{
		`{"kind":"webpush","endpoint":"https://push.example.org/x"}`,
		`{"endpoint":"https://push.example.org/x"}`,
		`{"kind":"unifiedpush","endpoint":"https://push.example.org/x","keys":{}}`,
	} {
		if _, err := DecodeArgs(OpPushSubscribe, json.RawMessage(raw)); err == nil {
			t.Errorf("%s taken", raw)
		}
	}
	if _, err := DecodeArgs(OpPushUnsubscribe, json.RawMessage(`{"kind":"unifiedpush"}`)); err != nil {
		t.Errorf("unsubscribe: %v", err)
	}
	if _, err := DecodeArgs(OpPushUnsubscribe, json.RawMessage(`{"kind":"other"}`)); err == nil {
		t.Error("unsubscribe of an unknown kind taken")
	}
}

// Spec (MOB-32): the wake event the hub sends on the control connection
// carries a count only, never what waits.
func TestWakeNoteIsCountOnly(t *testing.T) {
	b, err := json.Marshal(WakeNote{Count: 3})
	if err != nil || string(b) != `{"count":3}` {
		t.Fatalf("%s %v", b, err)
	}
	if EvWake != "wake" {
		t.Fatalf("EvWake %q", EvWake)
	}
}
