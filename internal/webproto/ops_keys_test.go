package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-7): the laptop's key bindings from the web (Preferences:
// Customize Keybindings). The page lists every laptop action with its
// keys (viewer); binding a key is the user's own preference, saved at
// once in keymap.json (controller, audited). A key another action has
// is only taken with replace.
func TestKeyBindingOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpKeysList: {RoleViewer, AuditNo, `{}`},
		OpKeysBind: {RoleController, AuditYes, `{"action":"PickTheme","key":"ctrl+alt+t"}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	for _, good := range []string{`{"action":"Save","key":"ctrl++","replace":true}`, `{"action":"Save","key":"f5"}`, `{"action":"Save","key":"alt+é"}`} {
		if _, err := DecodeArgs(OpKeysBind, json.RawMessage(good)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	for _, bad := range []string{`{"action":"","key":"f5"}`, `{"action":"Pick Theme","key":"f5"}`, `{"action":"Save","key":""}`,
		`{"action":"Save","key":"ctrl+\u0007"}`, `{"action":"Save","key":"` + strings.Repeat("x", 40) + `"}`, `{"action":"` + strings.Repeat("A", 80) + `","key":"f5"}`} {
		if _, err := DecodeArgs(OpKeysBind, json.RawMessage(bad)); err == nil {
			t.Errorf("bind %s: accepted", bad)
		}
	}
}
