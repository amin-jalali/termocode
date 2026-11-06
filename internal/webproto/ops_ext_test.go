package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-7): the laptop's Lua extension panels and status items on
// the web. The status topic names the panels and the status items' text;
// ext.panel renders one panel's lines (viewer); a row picked and a
// status item clicked run the extension's own code, as a click on the
// laptop does (controller, audited).
func TestExtOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpExtPanel:     {RoleViewer, AuditNo, `{"id":"good.main"}`},
		OpExtSelect:    {RoleController, AuditYes, `{"id":"good.main","row":2}`},
		OpExtStatusRun: {RoleController, AuditYes, `{"id":"good.st"}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
	}
	if _, ok := ResultOf(OpExtPanel); !ok {
		t.Error("ext.panel has no result type")
	}
	for _, bad := range []string{`{"id":""}`, `{"id":"a b"}`, `{"id":"a\u0000"}`, `{"id":"` + strings.Repeat("x", 200) + `"}`} {
		if _, err := DecodeArgs(OpExtPanel, json.RawMessage(bad)); err == nil {
			t.Errorf("panel %s: accepted", bad)
		}
	}
	for _, bad := range []string{`{"id":"good.main","row":-1}`, `{"id":"good.main","row":100000}`} {
		if _, err := DecodeArgs(OpExtSelect, json.RawMessage(bad)); err == nil {
			t.Errorf("select %s: accepted", bad)
		}
	}
}
