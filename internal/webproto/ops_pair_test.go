package webproto

import (
	"errors"
	"testing"
)

// Spec (web pairing): Pair a Device… and Devices… from the web are for a
// controller only: a viewer may call none of them, and the control
// connection never sends them.
func TestPairOpsControllerOnly(t *testing.T) {
	for _, op := range []string{"web.pair_open", "web.pair_close", "web.pair_answer", "web.devices", "web.device_remove"} {
		if !Allowed(RoleController, op) {
			t.Errorf("%s: a controller may not call it", op)
		}
		if Allowed(RoleViewer, op) || Allowed(RoleCtl, op) {
			t.Errorf("%s: a viewer or the control connection may call it", op)
		}
	}
	// Remove asks first; the rest answer at once.
	if p, _ := Lookup("web.device_remove"); !p.LaptopCard {
		t.Error("web.device_remove does not ask first")
	}
}

// The answer is a role or nothing (Not now); a device is named by its id.
func TestPairArgsAreChecked(t *testing.T) {
	for _, tc := range []struct{ op, args string }{
		{"web.pair_answer", `{"role":"owner"}`},
		{"web.pair_answer", `{"role":"controller"}`},
		{"web.device_remove", `{"id":""}`},
		{"web.device_remove", `{"id":"../devices.json"}`},
		{"web.device_remove", `{"id":"dev-AB"}`},
		{"web.pair_open", `{"guest":true}`},
	} {
		if _, err := DecodeArgs(tc.op, []byte(tc.args)); err == nil || errors.Is(err, ErrUnknownOp) {
			t.Errorf("%s %s: %v, want refused", tc.op, tc.args, err)
		}
	}
	for _, tc := range []struct{ op, args string }{
		{"web.pair_answer", `{}`},
		{"web.pair_answer", `{"role":"viewer"}`},
		{"web.pair_answer", `{"role":"control"}`},
		{"web.device_remove", `{"id":"dev-1a2b3c4d"}`},
	} {
		if _, err := DecodeArgs(tc.op, []byte(tc.args)); err != nil {
			t.Errorf("%s %s: %v", tc.op, tc.args, err)
		}
	}
}
