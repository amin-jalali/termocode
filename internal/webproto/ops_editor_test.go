package webproto

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Spec (WEBP-9, Editor and files): go to type definition and go to
// implementation read like go to definition (viewers, one position at a
// tick); buf.format and buf.reload change a buffer the page opened
// (controllers, audited; reload drops unsaved changes, so the page asks
// first, Cancel first); buf.markdown reads a buffer as a Markdown tree
// for the preview (viewers).
func TestEditorOps(t *testing.T) {
	for _, op := range []string{OpLspTypeDefinition, OpLspImplementation} {
		if _, err := DecodeArgs(op, json.RawMessage(`{"buf":3,"tick":9,"line":12,"col":4}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		if _, err := DecodeArgs(op, json.RawMessage(`{"buf":3,"tick":-1,"line":0,"col":0}`)); err == nil {
			t.Errorf("%s: a bad tick is accepted", op)
		}
		p, ok := Lookup(op)
		if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v: a v2 read for viewers", op, p)
		}
		if r, ok := ResultOf(op); !ok || r != reflect.TypeOf(LspLocations{}) {
			t.Errorf("%s: result %v, want LspLocations", op, r)
		}
	}
	if _, err := DecodeArgs(OpBufFormat, json.RawMessage(`{"buf":2,"tick":5}`)); err != nil {
		t.Errorf("format: %v", err)
	}
	for _, bad := range []string{`{"buf":0,"tick":5}`, `{"buf":2,"tick":-1}`, `{"buf":2}`, `{"buf":2,"tick":5,"x":1}`} {
		if _, err := DecodeArgs(OpBufFormat, json.RawMessage(bad)); err == nil {
			t.Errorf("format %s: accepted", bad)
		}
	}
	for _, op := range []string{OpBufFormat, OpBufReload} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v: an audited v2 edit for controllers", op, p)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	if _, err := DecodeArgs(OpBufReload, json.RawMessage(`{"buf":2}`)); err != nil {
		t.Errorf("reload: %v", err)
	}
	p, ok := Lookup(OpBufMarkdown)
	if !ok || p.Role != RoleViewer || p.Audit != AuditNo || p.Phase != PhaseV2 {
		t.Errorf("markdown row %+v", p)
	}
	if _, ok := ResultOf(OpBufMarkdown); !ok {
		t.Error("buf.markdown has no result type")
	}
}
