package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-7): the laptop's saved layouts from the web (Layout:
// Open…, Save As…, Delete…, Reset to Default). Listing is for a viewer;
// opening, saving and resetting change the laptop's screen or the
// user's own layout files (controller, audited); deleting a saved
// layout file waits for a laptop card.
func TestLayoutOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		card  bool
		audit Audit
		args  string
	}{
		OpLayoutList:   {RoleViewer, false, AuditNo, `{}`},
		OpLayoutOpen:   {RoleController, false, AuditYes, `{"name":"focus"}`},
		OpLayoutSave:   {RoleController, false, AuditYes, `{"name":"my review","scope":"project"}`},
		OpLayoutDelete: {RoleController, true, AuditYes, `{"name":"mine"}`},
		OpLayoutReset:  {RoleController, false, AuditYes, `{}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.LaptopCard != w.card || p.Audit != w.audit || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	if _, err := DecodeArgs(OpLayoutSave, json.RawMessage(`{"name":"mine","scope":"user","replace":true}`)); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{`{"name":"","scope":"user"}`, `{"name":"../x","scope":"user"}`, `{"name":" lead","scope":"user"}`, `{"name":"a/b","scope":"user"}`,
		`{"name":"` + strings.Repeat("x", 41) + `","scope":"user"}`, `{"name":"mine","scope":"team"}`, `{"name":"end.","scope":"user"}`} {
		if _, err := DecodeArgs(OpLayoutSave, json.RawMessage(bad)); err == nil {
			t.Errorf("save %s: accepted", bad)
		}
	}
	if _, err := DecodeArgs(OpLayoutOpen, json.RawMessage(`{"name":"a\u0000"}`)); err == nil {
		t.Error("open: a control byte is accepted")
	}
}
