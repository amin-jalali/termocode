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
