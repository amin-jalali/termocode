package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// Spec (web UI plan, language features): lsp.hover, lsp.complete,
// lsp.definition and lsp.references ask about one position of one buffer
// at a changedtick; all four only read, so viewers may call them, and
// none is audited or waits for the laptop.
func TestLspArgsRulesAndRows(t *testing.T) {
	ops := []string{OpLspHover, OpLspComplete, OpLspDefinition, OpLspReferences}
	for _, op := range ops {
		for _, args := range []string{
			`{"buf":0,"tick":1,"line":0,"col":0}`,
			`{"buf":1,"tick":-1,"line":0,"col":0}`,
			`{"buf":1,"tick":1,"line":-1,"col":0}`,
			`{"buf":1,"tick":1,"line":0,"col":-2}`,
			`{"buf":1,"tick":1,"line":0,"col":0,"path":"x"}`,
		} {
			if _, err := DecodeArgs(op, json.RawMessage(args)); err == nil {
				t.Errorf("%s %s: accepted", op, args)
			}
		}
		if _, err := DecodeArgs(op, json.RawMessage(`{"buf":3,"tick":9,"line":12,"col":4}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		p, ok := Lookup(op)
		if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v: a v2 read for viewers", op, p)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	if _, err := DecodeArgs(OpLspHover, json.RawMessage(`{"buf":1,"tick":1,"line":0,"col":-1}`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative col: %v, want ErrInvalid", err)
	}
}

// Spec: the diagnostics topic exists (v2, not keyed) and has its data
// types for the generated page types.
func TestDiagnosticsTopic(t *testing.T) {
	tp, err := ParseTopic("diagnostics")
	if err != nil || tp.Name != "diagnostics" {
		t.Fatalf("diagnostics: %v %+v", err, tp)
	}
	if _, err := ParseTopic("diagnostics:3"); err == nil {
		t.Error("diagnostics takes no key")
	}
	d, ok := topicDef("diagnostics")
	if !ok || d.Phase != PhaseV2 {
		t.Errorf("def %+v", d)
	}
}
