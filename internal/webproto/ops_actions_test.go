package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-9, Editor and files): lsp.code_actions lists the quick
// fixes of a range (viewers may look); lsp.code_action_apply applies one
// of the list on the laptop at the tick it was asked at (a controller,
// audited); lsp.inlay_hints reads the hints of some rows.
func TestCodeActionAndHintOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpLspCodeActions:     {RoleViewer, AuditNo, `{"buf":2,"tick":5,"line":3,"col":1,"endLine":3,"endCol":4}`},
		OpLspCodeActionApply: {RoleController, AuditYes, `{"buf":2,"tick":5,"index":0}`},
		OpLspInlayHints:      {RoleViewer, AuditNo, `{"buf":2,"tick":5,"first":0,"last":40}`},
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
	for op, bad := range map[string][]string{
		OpLspCodeActions:     {`{"buf":2,"tick":5,"line":3,"col":1,"endLine":2,"endCol":0}`, `{"buf":2,"tick":-1,"line":0,"col":0,"endLine":0,"endCol":0}`},
		OpLspCodeActionApply: {`{"buf":2,"tick":5,"index":-1}`, `{"buf":2,"tick":0,"index":0}`},
		OpLspInlayHints:      {`{"buf":2,"tick":5,"first":4,"last":3}`, `{"buf":2,"tick":5,"first":0,"last":100000}`},
	} {
		for _, a := range bad {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, a)
			}
		}
	}
}
