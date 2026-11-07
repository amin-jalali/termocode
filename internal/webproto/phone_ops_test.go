package webproto

import (
	"encoding/json"
	"testing"
	"time"
)

// Spec (MOBUX-20): the This phone ops are the gateway's own (it holds the
// device list): a viewer may call them for its own device, the hub never
// sees them; reading is not audited, a change and a forget are, and a
// forget is slow (one per 5 s). phone.get has a typed answer.
func TestPhoneOps(t *testing.T) {
	for _, c := range []struct {
		op    string
		audit Audit
	}{{OpPhoneGet, AuditNo}, {OpPhonePrefs, AuditYes}, {OpPhoneForget, AuditYes}} {
		p, ok := Lookup(c.op)
		if !ok || !p.Gateway || p.Role != RoleViewer || p.Audit != c.audit || p.LaptopCard {
			t.Errorf("%s: row %+v", c.op, p)
		}
	}
	if p, _ := Lookup(OpPhoneForget); p.Rate.Per/time.Duration(p.Rate.N) < 5*time.Second {
		t.Errorf("phone.forget rate %+v, want at most one per 5 s", p.Rate)
	}
	if _, ok := ResultOf(OpPhoneGet); !ok {
		t.Error("phone.get has no result type")
	}
	for _, op := range []string{OpPhoneGet, OpPhoneForget} {
		if _, err := DecodeArgs(op, json.RawMessage(`{"device":"dev-other"}`)); err == nil {
			t.Errorf("%s takes a device id: it must act on its own device only", op)
		}
	}
}

// Spec (MOBUX-20): quiet hours are both "HH:MM" on a 24-hour clock, or
// both empty (none); the start is not the end; the UTC offset is within
// ±14 h. Unknown fields are refused.
func TestPhonePrefsArgs(t *testing.T) {
	for _, good := range []string{
		`{"alerts":true,"quiet_from":"22:00","quiet_to":"07:00","utc_offset_min":60,"lock":true}`,
		`{"alerts":false}`,
		`{"alerts":true,"quiet_from":"00:00","quiet_to":"23:59","utc_offset_min":-840}`,
	} {
		if _, err := DecodeArgs(OpPhonePrefs, json.RawMessage(good)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{
		`{"alerts":true,"quiet_from":"22:00"}`,
		`{"alerts":true,"quiet_to":"07:00"}`,
		`{"alerts":true,"quiet_from":"24:00","quiet_to":"07:00"}`,
		`{"alerts":true,"quiet_from":"7:00","quiet_to":"08:00"}`,
		`{"alerts":true,"quiet_from":"22:60","quiet_to":"07:00"}`,
		`{"alerts":true,"quiet_from":"08:00","quiet_to":"08:00"}`,
		`{"alerts":true,"quiet_from":"22:0a","quiet_to":"07:00"}`,
		`{"alerts":true,"utc_offset_min":900}`,
		`{"alerts":true,"device":"dev-other"}`,
		`{"alerts":"yes"}`,
	} {
		if _, err := DecodeArgs(OpPhonePrefs, json.RawMessage(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}
