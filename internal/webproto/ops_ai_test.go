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

// Spec (WEBP-3): the agent worktrees list and a changed file's two texts
// are reads (a viewer may look, as at git diffs); Merge and Discard are
// controller ops that run only after a laptop card. A worktree is named
// by its id: never a path or an option.
func TestAIWorktreeOps(t *testing.T) {
	for _, op := range []string{OpAIWorktrees, OpAIWorktreeText} {
		if p, ok := Lookup(op); !ok || p.Role != RoleViewer || p.LaptopCard {
			t.Errorf("%s: %+v", op, p)
		}
	}
	for _, op := range []string{OpAIWorktreeMerge, OpAIWorktreeDiscard} {
		if p, ok := Lookup(op); !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
			t.Errorf("%s: %+v", op, p)
		}
		if _, err := DecodeArgs(op, []byte(`{"id":"r1-left.2_x"}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		for _, b := range []string{`{"id":""}`, `{"id":"-f"}`, `{"id":".."}`, `{"id":"a/b"}`, `{"id":"a b"}`, `{"id":"` + strings.Repeat("a", 65) + `"}`} {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %s accepted", op, b)
			}
		}
	}
	if _, err := DecodeArgs(OpAIWorktreeText, []byte(`{"id":"r1-a","path":"src/a.go"}`)); err != nil {
		t.Error(err)
	}
	for _, b := range []string{`{"id":"r1-a","path":"../x"}`, `{"id":"r1-a","path":"/etc/passwd"}`, `{"id":"r1-a","path":"src/"}`, `{"id":"r 1","path":"a"}`} {
		if _, err := DecodeArgs(OpAIWorktreeText, []byte(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
}

// Spec (WEBP-3, web design 3.3): the page may only ask to use another
// profile or model; the laptop decides with a card. A name has no
// control characters.
func TestAIUseOps(t *testing.T) {
	for _, op := range []string{OpAIUseProfile, OpAIUseModel} {
		if p, ok := Lookup(op); !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
			t.Errorf("%s: %+v", op, p)
		}
		if _, err := DecodeArgs(op, []byte(`{"name":"review"}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		for _, b := range []string{`{"name":""}`, `{"name":"a\nb"}`, `{"name":"` + strings.Repeat("a", MaxAIName+1) + `"}`} {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %q accepted", op, b)
			}
		}
	}
	if _, ok := TopicDataOf("ai"); !ok {
		t.Error("no ai topic data")
	}
}
