package webproto

import "testing"

// Spec (WEBP-3): ghost text and next edit suggestions are controller ops
// (they spend the user's tokens), counted in the audit, never a laptop
// card; a viewer may not call them. The args are a buffer, a changedtick
// and a place in it, nothing else.
func TestAICompletionOps(t *testing.T) {
	for _, op := range []string{OpAIComplete, OpAINextEdit} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditCounted || p.Live == 0 {
			t.Errorf("%s: %+v", op, p)
		}
		if Allowed(RoleViewer, op) {
			t.Errorf("%s: a viewer may call it", op)
		}
	}
	good := map[string][]string{
		OpAIComplete: {`{"buf":3,"tick":12,"line":4,"col":7}`, `{"buf":3,"tick":0,"line":0,"col":0,"manual":true}`},
		OpAINextEdit: {`{"buf":3,"tick":12,"line":4}`, `{"buf":3,"tick":1,"line":0,"seed":true}`},
	}
	bad := map[string][]string{
		OpAIComplete: {`{"buf":0,"tick":1,"line":0,"col":0}`, `{"buf":3,"tick":-1,"line":0,"col":0}`, `{"buf":3,"tick":1,"line":-1,"col":0}`, `{"buf":3,"tick":1,"line":0,"col":-2}`, `{"buf":3,"tick":1,"line":0,"col":0,"text":"x"}`},
		OpAINextEdit: {`{"buf":-1,"tick":1,"line":0}`, `{"buf":3,"tick":1,"line":3000000000}`, `{"buf":3,"tick":1,"line":0,"lines":["x"]}`},
	}
	for op, bs := range good {
		for _, b := range bs {
			if _, err := DecodeArgs(op, []byte(b)); err != nil {
				t.Errorf("%s %s: %v", op, b, err)
			}
		}
	}
	for op, bs := range bad {
		for _, b := range bs {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %s accepted", op, b)
			}
		}
	}
}
