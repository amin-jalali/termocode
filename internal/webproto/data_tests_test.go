package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-4, Test Explorer): the tests topic exists (v2, not keyed);
// tests.output and tests.refresh are reads for viewers; run, debug and
// stop need a controller, are audited and wait for no laptop card.
func TestTestsTopicAndRows(t *testing.T) {
	if tp, err := ParseTopic("tests"); err != nil || tp.Name != "tests" {
		t.Fatalf("tests: %v %+v", err, tp)
	}
	if _, err := ParseTopic("tests:1"); err == nil {
		t.Error("tests takes no key")
	}
	want := map[string]Role{OpTestsOutput: RoleViewer, OpTestsRefresh: RoleViewer, OpTestsRun: RoleController, OpTestsDebug: RoleController, OpTestsStop: RoleController}
	for op, role := range want {
		p, ok := Lookup(op)
		if !ok || p.Role != role || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if role == RoleController && p.Audit != AuditYes {
			t.Errorf("%s is not audited", op)
		}
	}
	if _, ok := ResultOf(OpTestsOutput); !ok {
		t.Error("tests.output has no result type")
	}
}

// Spec: a test id is "p:", "f:" or "c:" and a workspace path ("c:" with
// "::" and a name); no absolute path, no "..", no control character.
func TestTestIDArgs(t *testing.T) {
	good := []string{`{"id":"p:"}`, `{"id":"p:internal/app"}`, `{"id":"f:a_test.go"}`, `{"id":"c:pkg/a_test.go::TestA/sub case"}`}
	for _, a := range good {
		if _, err := DecodeArgs(OpTestsDebug, json.RawMessage(a)); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	bad := []string{`{"id":""}`, `{"id":"x:a"}`, `{"id":"f:"}`, `{"id":"f:/etc/passwd"}`, `{"id":"f:../a_test.go"}`,
		`{"id":"c:a_test.go"}`, `{"id":"c:a_test.go::"}`, `{"id":"c:a\u0000.go::T"}`, `{"id":"p:a","x":1}`}
	for _, a := range bad {
		if _, err := DecodeArgs(OpTestsDebug, json.RawMessage(a)); err == nil {
			t.Errorf("%s: accepted", a)
		}
	}
	for _, a := range []string{`{}`, `{"failed":true}`, `{"id":"f:a_test.go"}`} {
		if _, err := DecodeArgs(OpTestsRun, json.RawMessage(a)); err != nil {
			t.Errorf("run %s: %v", a, err)
		}
	}
	for _, a := range []string{`{}`, `{"id":"c:a_test.go::T"}`} {
		if _, err := DecodeArgs(OpTestsOutput, json.RawMessage(a)); err != nil {
			t.Errorf("output %s: %v", a, err)
		}
	}
	if _, err := DecodeArgs(OpTestsOutput, json.RawMessage(`{"id":"f:../x"}`)); err == nil {
		t.Error("output: a path out of the workspace")
	}
	if _, err := DecodeArgs(OpTestsRun, json.RawMessage(`{"id":"f:a_test.go","failed":true}`)); err == nil {
		t.Error("run: an id and failed together")
	}
}
