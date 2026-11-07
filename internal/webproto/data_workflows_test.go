package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WFX-19, plan §5.7, §6.10): the workflows topic (v2) lists the
// workflows and their recent runs with a short live tail; it carries no
// file contents and no secrets. workflows.stop needs a controller. The
// write gate of a run is a cards topic card of kind "workflow" (Deny
// first, Allow once; never "Allow for this workflow" from the web).
func TestWorkflowsTopicAndRows(t *testing.T) {
	if tp, err := ParseTopic("workflows"); err != nil || tp.Name != "workflows" {
		t.Fatalf("workflows: %v %+v", err, tp)
	}
	p, ok := Lookup(OpWorkflowsStop)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Fatalf("workflows.stop row %+v", p)
	}
	for _, a := range []string{`{"run":""}`, `{"run":"a/b"}`, `{"run":"x\ny"}`, `{"run":"20261106-090000","cmd":"rm"}`} {
		if _, err := DecodeArgs(OpWorkflowsStop, json.RawMessage(a)); err == nil {
			t.Errorf("stop %s: accepted", a)
		}
	}
	if _, err := DecodeArgs(OpWorkflowsStop, json.RawMessage(`{"run":"20261106-090000.123"}`)); err != nil {
		t.Errorf("stop: %v", err)
	}
	ts, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"workflows": { snap: WorkflowsSnap; delta: WorkflowsSnap };`, `"tool" | "mcp" | "plan" | "review" | "trust" | "laptop" | "workflow"`} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	for _, field := range []string{"Data", "Path", "File", "Body", "Content"} {
		if strings.Contains(string(ts), "export interface WorkflowItem {") && strings.Contains(workflowsTSBlock(string(ts)), strings.ToLower(field)+":") {
			t.Errorf("the workflow rows carry %s", field)
		}
	}
}

// workflowsTSBlock is the TypeScript of the workflow row types.
func workflowsTSBlock(ts string) string {
	var b strings.Builder
	for _, name := range []string{"WorkflowItem", "WorkflowRun"} {
		i := strings.Index(ts, "export interface "+name+" {")
		if i < 0 {
			continue
		}
		j := strings.Index(ts[i:], "\n}")
		b.WriteString(ts[i : i+j])
	}
	return b.String()
}

// Spec (MOB-60, mobile plan §3.9): the phone's workflow ops. dry_run
// needs a controller and answers with the run id; run and save need a
// controller and a laptop card (they answer with the card id). Names
// follow the file name rule; a saved file is non-empty text of at most
// MaxWorkflowSave bytes; unknown keys are refused.
func TestWorkflowsPhoneOpRows(t *testing.T) {
	for op, card := range map[string]bool{OpWorkflowsDryRun: false, OpWorkflowsRun: true, OpWorkflowsSave: true} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard != card || p.Audit != AuditYes || p.Phase != PhaseV2 {
			t.Fatalf("%s row %+v", op, p)
		}
	}
	if r, ok := ResultOf(OpWorkflowsDryRun); !ok || r.Name() != "WorkflowRunArgs" {
		t.Errorf("dry_run result %v", r)
	}
	for _, op := range []string{OpWorkflowsRun, OpWorkflowsSave} {
		if r, ok := ResultOf(op); !ok || r.Name() != "AskRef" {
			t.Errorf("%s result %v", op, r)
		}
	}
	for _, op := range []string{OpWorkflowsDryRun, OpWorkflowsRun} {
		for _, a := range []string{`{"name":""}`, `{"name":"Mine"}`, `{"name":"9x"}`, `{"name":"a/b"}`, `{"name":"a b"}`,
			`{"name":"` + strings.Repeat("a", MaxWorkflowName+1) + `"}`, `{"name":"mine","dry":true}`} {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, a)
			}
		}
		if _, err := DecodeArgs(op, json.RawMessage(`{"name":"docs-report_2"}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
	}
	big, _ := json.Marshal(strings.Repeat("a", MaxWorkflowSave+1))
	for _, a := range []string{`{"name":"x","content":""}`, `{"name":"x","content":"  \n"}`, `{"name":"X","content":"{}"}`,
		`{"name":"x","content":"a\u0000b"}`, `{"name":"x","content":` + string(big) + `}`, `{"name":"x","content":"{}","path":"/etc"}`} {
		if _, err := DecodeArgs(OpWorkflowsSave, json.RawMessage(a)); err == nil {
			t.Errorf("save %.80s: accepted", a)
		}
	}
	if _, err := DecodeArgs(OpWorkflowsSave, json.RawMessage(`{"name":"fresh","content":"{ \"version\": 1 }"}`)); err != nil {
		t.Errorf("save: %v", err)
	}
}
