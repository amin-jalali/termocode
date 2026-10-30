package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-4, Run and Debug): the debug topic exists (v2, not keyed);
// looking at variables and loaded scripts is open to viewers; starting,
// stepping, breakpoints, watches, evaluating and frames need a
// controller; nothing waits for a laptop card.
func TestDebugTopicAndRows(t *testing.T) {
	if tp, err := ParseTopic("debug"); err != nil || tp.Name != "debug" {
		t.Fatalf("debug: %v %+v", err, tp)
	}
	want := map[string]Role{OpDebugExpand: RoleViewer, OpDebugSources: RoleViewer, OpDebugStart: RoleController, OpDebugRun: RoleController,
		OpDebugSelect: RoleController, OpDebugControl: RoleController, OpDebugBp: RoleController, OpDebugWatch: RoleController,
		OpDebugEval: RoleController, OpDebugFrame: RoleController}
	for op, role := range want {
		p, ok := Lookup(op)
		if !ok || p.Role != role || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
	for _, op := range []string{OpDebugStart, OpDebugRun, OpDebugBp, OpDebugWatch, OpDebugEval} {
		if p, _ := Lookup(op); p.Audit != AuditYes {
			t.Errorf("%s is not audited", op)
		}
	}
}

// Spec: the args take fixed steps and ops only, workspace paths, and
// text without control characters; a condition only with set.
func TestDebugArgsRules(t *testing.T) {
	good := map[string][]string{
		OpDebugControl: {`{"op":"continue"}`, `{"op":"stop"}`},
		OpDebugBp: {`{"op":"toggle","path":"a.go","line":3}`, `{"op":"set","path":"a.go","line":3,"condition":"i > 2","hit":"3","log":"i={i}"}`,
			`{"op":"clear"}`, `{"op":"clear","path":"a.go"}`},
		OpDebugExpand: {`{"key":"s:Locals","open":true}`, `{"key":"s:Locals/a/b","open":false}`},
		OpDebugWatch:  {`{"op":"add","expr":"x + 1"}`, `{"op":"remove","expr":"x"}`},
		OpDebugEval:   {`{"expr":"len(xs)"}`},
		OpDebugStart:  {`{}`, `{"config":"Debug main"}`},
		OpDebugFrame:  {`{"id":0}`},
	}
	bad := map[string][]string{
		OpDebugControl: {`{"op":"eval"}`, `{}`},
		OpDebugBp: {`{"op":"toggle","path":"/etc/x.go","line":3}`, `{"op":"toggle","path":"../a.go","line":3}`, `{"op":"toggle","path":"a.go","line":0}`,
			`{"op":"toggle","path":"a.go","line":3,"condition":"x"}`, `{"op":"clear","line":3}`, `{"op":"wipe"}`,
			`{"op":"set","path":"a.go","line":3,"log":"a\nb"}`},
		OpDebugExpand: {`{"key":"Locals"}`, `{"key":"s:"}`, `{"key":"s:a\u0001"}`},
		OpDebugWatch:  {`{"op":"add","expr":"  "}`, `{"op":"edit","expr":"x"}`},
		OpDebugEval:   {`{"expr":""}`, `{"expr":"a\u001b[2J"}`},
		OpDebugStart:  {`{"config":"a\nb"}`},
		OpDebugFrame:  {`{"id":-1}`},
	}
	for op, list := range good {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err != nil {
				t.Errorf("%s %s: %v", op, a, err)
			}
		}
	}
	for op, list := range bad {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, a)
			}
		}
	}
}
