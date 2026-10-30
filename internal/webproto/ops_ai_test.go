package webproto

import (
	"strings"
	"testing"
)

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

// Spec (WEBP-3): ai.action (Explain, Fix, Edit, Doc) is a controller op,
// audited, one at a time; Edit needs a one-line instruction, the others
// take none; a range ends after it starts.
func TestAIActionOp(t *testing.T) {
	p, ok := Lookup(OpAIAction)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Live != 1 {
		t.Fatalf("ai.action: %+v", p)
	}
	for _, b := range []string{
		`{"buf":3,"tick":5,"kind":"explain","start":2,"end":2,"line":2}`,
		`{"buf":3,"tick":5,"kind":"fix","start":0,"end":4,"line":1}`,
		`{"buf":3,"tick":5,"kind":"doc","start":7,"end":7,"line":7}`,
		`{"buf":3,"tick":5,"kind":"edit","start":1,"end":3,"line":1,"instruction":"use a map"}`,
	} {
		if _, err := DecodeArgs(OpAIAction, []byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range []string{
		`{"buf":3,"tick":5,"kind":"run","start":0,"end":0,"line":0}`,
		`{"buf":3,"tick":5,"kind":"fix","start":4,"end":2,"line":0}`,
		`{"buf":3,"tick":5,"kind":"edit","start":0,"end":1,"line":0}`,
		`{"buf":3,"tick":5,"kind":"edit","start":0,"end":1,"line":0,"instruction":"  "}`,
		`{"buf":3,"tick":5,"kind":"edit","start":0,"end":1,"line":0,"instruction":"a\nb"}`,
		`{"buf":3,"tick":5,"kind":"explain","start":0,"end":1,"line":0,"instruction":"why"}`,
		`{"buf":3,"tick":5,"kind":"edit","start":0,"end":1,"line":0,"instruction":"` + strings.Repeat("a", MaxAIInstruction+1) + `"}`,
	} {
		if _, err := DecodeArgs(OpAIAction, []byte(b)); err == nil {
			t.Errorf("%.80s accepted", b)
		}
	}
}
